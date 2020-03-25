package externalservice

import (
	"context"
	"fmt"
	"strconv"

	"github.com/go-logr/logr"
	submarinerv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	"github.com/mkimuram/k8s-ext-connector/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	yaml "gopkg.in/yaml.v2"
)

// blank assignment to verify that ReconcileExternalService implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileExternalService{}

// ReconcileExternalService reconciles a ExternalService object
type ReconcileExternalService struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a ExternalService object and makes changes based on the state read
// and what is in the ExternalService.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileExternalService) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling ExternalService")

	// Fetch the ExternalService instance
	instance := &submarinerv1alpha1.ExternalService{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// ExternalService CR is deleted, so clean up the related resources, then clear finalizer
	if instance.GetDeletionTimestamp() != nil {
		// Clean up related resources
		if err := r.deleteResourceForExternalService(instance); err != nil {
			return reconcile.Result{}, err
		}

		// Clear finalizer
		instance.SetFinalizers(nil)
		if err := r.client.Update(context.TODO(), instance); err != nil {
			return reconcile.Result{}, err
		}

		return reconcile.Result{}, nil
	}

	// Add finalizer for this CR
	if err := r.addFinalizer(reqLogger, instance); err != nil {
		return reconcile.Result{}, err
	}

	// Define a new forwarder Pod object
	pod := genForwardPodSpec(instance)

	// Check if this Pod already exists
	foundPod := &corev1.Pod{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, foundPod)
	if err != nil && !errors.IsNotFound(err) {
		return reconcile.Result{}, err
	} else if err != nil {
		// Ensure configmap exists before creating forwarder pod
		_, err := util.GetOrCreateConfigMap(r.client, instance.Name, ConnectorNamespace)
		if err != nil {
			return reconcile.Result{}, err
		}

		reqLogger.Info("Creating a new Pod", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)
		err = r.client.Create(context.TODO(), pod)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	// Define a new forwarder service object
	service := genForwardServiceSpec(instance)

	// Check if this service already exists
	foundSvc := &corev1.Service{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, foundSvc)
	if err != nil && !errors.IsNotFound(err) {
		return reconcile.Result{}, err
	} else if err != nil {
		reqLogger.Info("Creating a new service", "Service.Namespace", service.Namespace, "Serivce.Name", service.Name)
		err = r.client.Create(context.TODO(), service)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	// Update configmap for forwarder pod
	err = r.updateConfigmapDataForForwarder(instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Update configmap for gateways
	err = r.updateConfigmapDataForGateways(instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// updateConfigmapDataForForwarder updates configmap data for the CR
func (r *ReconcileExternalService) updateConfigmapDataForForwarder(cr *submarinerv1alpha1.ExternalService) error {
	// Get or create configmap to update
	config, err := util.GetOrCreateConfigMap(r.client, cr.Name, ConnectorNamespace)
	if err != nil {
		return err
	}

	// Update data
	data := r.genForwarderRules(cr)
	configmapData := map[string]string{"data.yaml": data}

	// Update configmap with the data
	if err := util.UpdateConfigmapData(r.client, config, configmapData); err != nil {
		return err
	}

	return nil
}

func (r *ReconcileExternalService) updateConfigmapDataForGateways(cr *submarinerv1alpha1.ExternalService) error {
	// Get list of externalService
	// TODO: Use selector to get only necessary external service inside the loop.
	// Currently, only namespace and name is allowd for field selector (k/k#53459).
	// So, we need to add labels and use label selector, instead.
	eslist := &submarinerv1alpha1.ExternalServiceList{}
	opts := []client.ListOption{}
	if err := r.client.List(context.TODO(), eslist, opts...); err != nil {
		return err
	}

	// Update all configmap of the gateway for sources
	for _, source := range cr.Spec.Sources {
		rules, err := r.getGwRulesForSource(eslist, source)
		if err != nil {
			// TODO: handle error properly
			continue
		}

		configMapName, err := util.GetRuleName(source.SourceIP)
		if err != nil {
			// TODO: handle error properly
			continue
		}

		configMap, err := util.GetOrCreateConfigMap(r.client, configMapName, ConnectorNamespace)
		if err != nil {
			// TODO: handle error properly
			continue
		}

		// Update rules
		configmapData := map[string]string{"rules": rules}

		// Update configmap
		if err := util.UpdateConfigmapData(r.client, configMap, configmapData); err != nil {
			// TODO: handle error properly
			continue
		}
	}

	return nil
}

func (r *ReconcileExternalService) getGwRulesForSource(
	eslist *submarinerv1alpha1.ExternalServiceList,
	source submarinerv1alpha1.Source,
) (string, error) {

	rules := ""
	// Loop over all service in externalService's sources
	for _, es := range eslist.Items {
		for _, src := range es.Spec.Sources {
			// Update is only needed for the same sourceIP
			if src.SourceIP != source.SourceIP {
				continue
			}
			rs, err := r.getGwRule(src, es.Name, es.Spec.TargetIP)
			if err != nil {
				// TODO: handle error properly
				continue
			}
			rules += rs
		}
	}
	return rules, nil
}

func (r *ReconcileExternalService) getGwRule(src submarinerv1alpha1.Source, esName, targetIP string) (string, error) {
	rules := ""

	// Get service for src
	svc := &corev1.Service{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: src.Service.Name, Namespace: src.Service.Namespace}, svc)
	if err != nil && !errors.IsNotFound(err) {
		// TODO: handle error properly
		return "", fmt.Errorf("")
	}

	// Get configmap for esName
	esConfig := &corev1.ConfigMap{}
	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: esName, Namespace: ConnectorNamespace}, esConfig); err != nil && !errors.IsNotFound(err) {
		// TODO: handle error properly
		return "", fmt.Errorf("")
	}
	for _, rport := range svc.Spec.Ports {
		remotePort := strconv.Itoa(int(rport.Port))
		// Find remoteFwdPort from esConfig which has combination of clusterIP, sourceIP, and remotePort
		remoteFwdPort, err := util.GetRemoteFwdPort(esConfig, svc.Spec.ClusterIP, src.SourceIP, remotePort)
		if err != nil {
			// TODO: handle error properly
			continue
		}
		rules += fmt.Sprintf("PREROUTING -t nat -m tcp -p tcp --dst %s --src %s --dport %s -j DNAT --to-destination %s:%s\n",
			src.SourceIP, targetIP, remotePort, src.SourceIP, remoteFwdPort)
		rules += fmt.Sprintf("POSTROUTING -t nat -m tcp -p tcp --dst %s --dport %s -j SNAT --to-source %s\n",
			targetIP, remoteFwdPort, src.SourceIP)
	}

	return rules, nil
}

func (r *ReconcileExternalService) genForwarderRules(cr *submarinerv1alpha1.ExternalService) string {
	usedPorts := map[string]string{}
	sshTunnelRules := r.genSSHTunnelRules(cr, usedPorts)
	remoteSSHTunnelRules := r.genRemoteSSHTunnelRules(cr)
	iptablesRules := r.genIptablesRules(cr, usedPorts)

	data := map[string]string{"ssh-tunnel": sshTunnelRules, "remote-ssh-tunnel": remoteSSHTunnelRules, "iptables-rule": iptablesRules}

	byteData, err := yaml.Marshal(data)
	if err != nil {
		// TODO: Fix me
		return ""
	}
	return string(byteData[:])
}

func (r *ReconcileExternalService) genSSHTunnelRules(cr *submarinerv1alpha1.ExternalService, usedPorts map[string]string) string {
	rules := ""

	for _, source := range cr.Spec.Sources {
		for _, port := range cr.Spec.Ports {
			fwdPort := util.GenPort(source.SourceIP, port.TargetPort.String(), usedPorts)
			// Skip generating rules if any of values are not available
			if fwdPort == "" || cr.Spec.TargetIP == "" || port.TargetPort.String() == "" || source.SourceIP == "" {
				continue
			}
			rules += fmt.Sprintf("%s:%s:%s %s\n", fwdPort, cr.Spec.TargetIP, port.TargetPort.String(), source.SourceIP)
		}
	}

	return rules
}

func (r *ReconcileExternalService) genRemoteSSHTunnelRules(cr *submarinerv1alpha1.ExternalService) string {
	rules := ""

	for _, source := range cr.Spec.Sources {
		svc := &corev1.Service{}
		err := r.client.Get(context.TODO(), types.NamespacedName{Name: source.Service.Name, Namespace: source.Service.Namespace}, svc)
		if err != nil && !errors.IsNotFound(err) {
			// TODO: Handle error properly
			continue
		}
		clusterIP := svc.Spec.ClusterIP

		usedRemotePorts, err := util.GetUsedRemotePorts(r.client, ConnectorNamespace, source.SourceIP)
		if err != nil {
			// TODO: Handle error properly
			continue
		}

		for _, svcPort := range svc.Spec.Ports {
			remoteFwdPort := util.GenRemotePort(strconv.Itoa(int(svcPort.Port)), usedRemotePorts)
			// Skip generating rules if any of values are not available
			if source.SourceIP == "" || remoteFwdPort == "" || clusterIP == "" || strconv.Itoa(int(svcPort.Port)) == "" {
				continue
			}
			rules += fmt.Sprintf("%s:%s:%s:%s %s\n", source.SourceIP, remoteFwdPort, clusterIP, strconv.Itoa(int(svcPort.Port)), source.SourceIP)
		}
	}

	return rules
}

func (r *ReconcileExternalService) genIptablesRules(cr *submarinerv1alpha1.ExternalService, usedPorts map[string]string) string {
	logger := log.WithValues("Namespace", cr.Namespace, "Name", cr.Name)
	logger.Info("genIptablesRules")

	rules := ""

	fwdPod := &corev1.Pod{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: cr.Name, Namespace: ConnectorNamespace}, fwdPod)
	if err != nil && !errors.IsNotFound(err) {
		// TODO: Handle error properly
		return ""
	}
	fwdPodIP := fwdPod.Status.PodIP
	logger.Info("fwdPod", "fwdPodIP", fwdPodIP)

	for _, source := range cr.Spec.Sources {
		logger.Info("service", "name", source.Service.Name, "namespace", source.Service.Namespace)
		endpoint := &corev1.Endpoints{}
		err := r.client.Get(context.TODO(), types.NamespacedName{Name: source.Service.Name, Namespace: source.Service.Namespace}, endpoint)
		if err != nil && !errors.IsNotFound(err) {
			// TODO: Handle error properly
			logger.Info("getEndpoint", "err", err)
			continue
		}
		logger.Info("getEndpoint", "endpoint", endpoint)
		for _, port := range cr.Spec.Ports {
			fwdPort := util.GetPort(source.SourceIP, port.TargetPort.String(), usedPorts)
			logger.Info("port", "port", port.TargetPort.String(), "fwdPort", fwdPort)

			for _, subset := range endpoint.Subsets {
				for _, addr := range subset.Addresses {
					logger.Info("Values:", "fwdPodIP", fwdPodIP, "IP", addr.IP, "TargetPort", port.TargetPort.String(), "fwdPort", fwdPort, "TargetIP", cr.Spec.TargetIP)
					// Skip generating rules if any of values are not available
					if fwdPodIP == "" || addr.IP == "" || port.TargetPort.String() == "" || fwdPort == "" || cr.Spec.TargetIP == "" {
						continue
					}
					// TODO: Also handle UDP properly
					rules += fmt.Sprintf("PREROUTING -t nat -m tcp -p tcp --dst %s --src %s --dport %s -j DNAT --to-destination %s:%s\n",
						fwdPodIP, addr.IP, port.TargetPort.String(), fwdPodIP, fwdPort)
					rules += fmt.Sprintf("POSTROUTING -t nat -m tcp -p tcp --dst %s --dport %s -j SNAT --to-source %s\n",
						cr.Spec.TargetIP, fwdPort, fwdPodIP)
				}
			}
		}
	}

	return rules
}

func (r *ReconcileExternalService) addFinalizer(reqLogger logr.Logger, cr *submarinerv1alpha1.ExternalService) error {
	if len(cr.GetFinalizers()) < 1 && cr.GetDeletionTimestamp() == nil {
		reqLogger.Info("Adding Finalizer to ExternalService")

		cr.SetFinalizers([]string{ExternalServiceFinalizerName})
		// Update CR
		if err := r.client.Update(context.TODO(), cr); err != nil {
			reqLogger.Error(err, "Failed to update ExternalService with finalizer")
			return err
		}

	}
	return nil
}

// deleteResourceForExternalService deletes all related resource for the external service
func (r *ReconcileExternalService) deleteResourceForExternalService(cr *submarinerv1alpha1.ExternalService) error {
	// Delete pod
	pod := &corev1.Pod{}
	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: cr.Name, Namespace: ConnectorNamespace}, pod); err != nil && !errors.IsNotFound(err) {
		return err
	} else if err == nil {
		// Pod exists, so delete it
		_ = r.client.Delete(context.Background(), pod)
	}

	// Delete service
	svc := &corev1.Service{}
	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: cr.Name, Namespace: ConnectorNamespace}, svc); err != nil && !errors.IsNotFound(err) {
		return err
	} else if err == nil {
		// Service exists, so delete it
		_ = r.client.Delete(context.Background(), svc)
	}

	// Delete configmap
	configMap := &corev1.ConfigMap{}
	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: cr.Name, Namespace: ConnectorNamespace}, configMap); err != nil && !errors.IsNotFound(err) {
		return err
	} else if err == nil {
		// Configmap exists, so delete it
		_ = r.client.Delete(context.Background(), configMap)
	}

	return nil
}
