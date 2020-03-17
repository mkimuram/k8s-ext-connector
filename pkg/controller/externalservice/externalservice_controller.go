package externalservice

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	submarinerv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	"github.com/mkimuram/k8s-ext-connector/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	yaml "gopkg.in/yaml.v2"
)

var log = logf.Log.WithName("controller_externalservice")

const (
	// ConnectorNamespace is the namespace to deploy external services
	ConnectorNamespace = "external-services"
	// ExternalServiceNamespaceLabel is the label for namespace of external service
	ExternalServiceNamespaceLabel = "externalservice.submariner.io/namespace"
	// ExternalServiceNameLabel is the label for name of external service
	ExternalServiceNameLabel = "externalservice.submariner.io/name"
	// ExternalServiceFinalizerName is the name of finalizer for external service
	ExternalServiceFinalizerName = "finalizer.externalservice.submariner.io"
	// MinPort is the smallest port number that can be used by forwarder pod
	MinPort = 2049
	// MaxPort is the biggest port number that can be used by forwarder pod
	MaxPort = 65536
)

// Add creates a new ExternalService Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileExternalService{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("externalservice-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource ExternalService
	err = c.Watch(&source.Kind{Type: &submarinerv1alpha1.ExternalService{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for forwarder pod
	// Cross-namespace owner references is not allowed, so using EnqueueRequestsFromMapFunc
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestsFromMapFunc{
		ToRequests: handler.ToRequestsFunc(func(a handler.MapObject) []reconcile.Request {
			pod := a.Object.(*corev1.Pod)
			requests := []reconcile.Request{}

			// Forwarder pod exists only in ConnectorNamespace
			if pod.Namespace != ConnectorNamespace {
				return requests
			}

			// Append external service to request only if the pod has the labels
			namespace, ok1 := pod.Labels[ExternalServiceNamespaceLabel]
			name, ok2 := pod.Labels[ExternalServiceNameLabel]
			if ok1 && ok2 {
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: namespace,
						Name:      name,
					},
				})
			}

			return requests
		}),
	})
	if err != nil {
		return err
	}

	// Watch for forwarder service
	// Cross-namespace owner references is not allowed, so using EnqueueRequestsFromMapFunc
	err = c.Watch(&source.Kind{Type: &corev1.Service{}}, &handler.EnqueueRequestsFromMapFunc{
		ToRequests: handler.ToRequestsFunc(func(a handler.MapObject) []reconcile.Request {
			svc := a.Object.(*corev1.Service)
			requests := []reconcile.Request{}

			// Forwarder service exists only in ConnectorNamespace
			if svc.Namespace != ConnectorNamespace {
				return requests
			}

			// Append external service to request only if the service has the labels
			namespace, ok1 := svc.Labels[ExternalServiceNamespaceLabel]
			name, ok2 := svc.Labels[ExternalServiceNameLabel]
			if ok1 && ok2 {
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: namespace,
						Name:      name,
					},
				})
			}

			return requests
		}),
	})
	if err != nil {
		return err
	}

	// Watch for endpoints
	err = c.Watch(&source.Kind{Type: &corev1.Endpoints{}}, &handler.EnqueueRequestsFromMapFunc{
		ToRequests: handler.ToRequestsFunc(func(a handler.MapObject) []reconcile.Request {
			ep := a.Object.(*corev1.Endpoints)
			requests := []reconcile.Request{}

			// Get list of externalService
			list := &submarinerv1alpha1.ExternalServiceList{}
			opts := []client.ListOption{}
			if err := mgr.GetClient().List(context.TODO(), list, opts...); err != nil {
				return requests
			}

			// Loop over all service in externalService's sources
			for _, es := range list.Items {
				for _, source := range es.Spec.Sources {
					// Append external service to request only if its namespace and name match to endpoint's ones
					if ep.Namespace == source.Service.Namespace && ep.Name == source.Service.Name {
						requests = append(requests, reconcile.Request{
							NamespacedName: types.NamespacedName{
								Namespace: es.Namespace,
								Name:      es.Name,
							},
						})
					}
				}
			}

			return requests
		}),
	})
	if err != nil {
		return err
	}

	return nil
}

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
	err = r.updateConfigmapDataForCR(instance)
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

// genForwardPodSpec returns a spec for a forwarder pod
func genForwardPodSpec(cr *submarinerv1alpha1.ExternalService) *corev1.Pod {
	labels := map[string]string{
		ExternalServiceNamespaceLabel: cr.Namespace,
		ExternalServiceNameLabel:      cr.Name,
	}
	isPrivileged := true
	var defaultMode int32 = 256

	env := []corev1.EnvVar{
		{
			Name:  "EXTERNAL_SERVICE_NAME",
			Value: cr.Name,
		},
		{
			Name:  "DATA_FILE",
			Value: "/etc/external-service/config/data.yaml",
		},
	}

	volumes := []corev1.Volume{
		{
			Name: "data-file",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cr.Name,
					},
				},
			},
		},
		{
			Name: "ssh-key-volume",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  "my-ssh-key",
					DefaultMode: &defaultMode,
				},
			},
		},
	}

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "data-file",
			MountPath: "/etc/external-service/config",
			ReadOnly:  true,
		},
		{
			Name:      "ssh-key-volume",
			MountPath: "/etc/ssh-key",
			ReadOnly:  true,
		},
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: ConnectorNamespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:            "forwarder",
					Image:           "forwarder:0.1",
					SecurityContext: &corev1.SecurityContext{Privileged: &isPrivileged},
					Env:             env,
					VolumeMounts:    volumeMounts,
				},
			},
			Volumes: volumes,
		},
	}
}

// genForwardServiceSpec returns a spec for a forwarder service
func genForwardServiceSpec(cr *submarinerv1alpha1.ExternalService) *corev1.Service {
	var ports []corev1.ServicePort

	labels := map[string]string{
		ExternalServiceNamespaceLabel: cr.Namespace,
		ExternalServiceNameLabel:      cr.Name,
	}

	for _, port := range cr.Spec.Ports {
		ports = append(ports, port)
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: ConnectorNamespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Ports:    ports,
			Selector: labels,
		},
	}
}

// updateConfigmapDataForCR updates configmap data for the CR
func (r *ReconcileExternalService) updateConfigmapDataForCR(cr *submarinerv1alpha1.ExternalService) error {
	// Get or create configmap to update
	config, err := util.GetOrCreateConfigMap(r.client, cr.Name, ConnectorNamespace)
	if err != nil {
		return err
	}

	// Update data
	data := r.genRules(cr)
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
	list := &submarinerv1alpha1.ExternalServiceList{}
	opts := []client.ListOption{}
	if err := r.client.List(context.TODO(), list, opts...); err != nil {
		return err
	}

	// Update all configmap of the gateway for sources
	for _, source := range cr.Spec.Sources {
		rules := ""

		// Loop over all service in externalService's sources
		for _, es := range list.Items {
			for _, src := range es.Spec.Sources {
				// Need to update configmap of the gateway for this sourceIP
				if src.SourceIP == source.SourceIP {
					targetIP := es.Spec.TargetIP
					sourceIP := src.SourceIP
					svc := &corev1.Service{}
					err := r.client.Get(context.TODO(), types.NamespacedName{Name: src.Service.Name, Namespace: src.Service.Namespace}, svc)
					if err != nil && !errors.IsNotFound(err) {
						// TODO: Handle error properly
						continue
					}

					clusterIP := svc.Spec.ClusterIP
					esConfig := &corev1.ConfigMap{}
					if err := r.client.Get(context.TODO(), types.NamespacedName{Name: es.Name, Namespace: ConnectorNamespace}, esConfig); err != nil && !errors.IsNotFound(err) {
						// TODO: handle error properly
						continue
					}
					for _, rport := range svc.Spec.Ports {
						remotePort := strconv.Itoa(int(rport.Port))
						remoteFwdPort, err := r.getRemoteFwdPort(esConfig, es.Name, clusterIP, sourceIP, remotePort)
						if err != nil {
							// TODO: handle error properly
							continue
						}
						rules += fmt.Sprintf("PREROUTING -t nat -m tcp -p tcp --dst %s --src %s --dport %s -j DNAT --to-destination %s:%s\n",
							sourceIP, targetIP, remotePort, sourceIP, remoteFwdPort)
						rules += fmt.Sprintf("POSTROUTING -t nat -m tcp -p tcp --dst %s --dport %s -j SNAT --to-source %s\n",
							targetIP, remoteFwdPort, sourceIP)
					}
				}
			}
		}

		// TODO: move getRuleName to util to share with gateway.go
		configMapName, err := getRuleName(source.SourceIP)
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

func (r *ReconcileExternalService) getRemoteFwdPort(esconfig *corev1.ConfigMap, esName, clusterIP, sourceIP, remotePort string) (string, error) {
	var remoteRules string
	var hasData bool

	if data, ok := esconfig.Data["data.yaml"]; ok {
		yamlData := make(map[string]map[string]map[string]string)
		err := yaml.Unmarshal([]byte(data), yamlData)
		if err != nil {
			return "", err
		}
		if forwarder, ok := yamlData["forwarder"]; ok {
			if rules, ok := forwarder[esName]; ok {
				remoteRules, hasData = rules["remote-ssh-tunnel"]
			}
		}
	}
	if !hasData {
		return "", fmt.Errorf("getRemoteFwdPort: configMap doesn't have data")
	}
	for _, s := range strings.Split(string(remoteRules), "\n") {
		// Fields are like below:
		// {sourceIP}:{remoteFwdPort}:{clusterIP}:{remotePort},{sourceIP}
		// ex)
		// 192.168.122.200:2049:10.96.223.183:80,192.168.122.200
		commas := strings.Split(s, ",")
		if len(commas) < 2 {
			continue
		}
		cols := strings.Split(commas[0], ":")
		if len(cols) < 4 {
			continue
		}
		if cols[0] == sourceIP && cols[2] == clusterIP && cols[3] == remotePort {
			return cols[1], nil
		}
	}

	return "", fmt.Errorf("getRemoteFwdPort: remoteFwdPort not found")
}

func (r *ReconcileExternalService) genRules(cr *submarinerv1alpha1.ExternalService) string {
	usedPorts := map[string]string{}
	sshTunnelRules := r.genSSHTunnelRules(cr, usedPorts)
	remoteSSHTunnelRules := r.genRemoteSSHTunnelRules(cr)
	iptablesRules := r.genIptablesRules(cr, usedPorts)

	data := map[string]map[string]map[string]string{"forwarder": {cr.Name: {"ssh-tunnel": sshTunnelRules, "remote-ssh-tunnel": remoteSSHTunnelRules, "iptables-rule": iptablesRules}}}

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
			fwdPort := genPort(source.SourceIP, port.TargetPort.String(), usedPorts)
			// Skip generating rules if any of values are not available
			if fwdPort == "" || cr.Spec.TargetIP == "" || port.TargetPort.String() == "" || source.SourceIP == "" {
				continue
			}
			rules += fmt.Sprintf("%s:%s:%s,%s\n", fwdPort, cr.Spec.TargetIP, port.TargetPort.String(), source.SourceIP)
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

		usedRemotePorts, err := r.getUsedRemotePorts(source.SourceIP)
		if err != nil {
			// TODO: Handle error properly
			continue
		}

		for _, svcPort := range svc.Spec.Ports {
			remoteFwdPort := genRemotePort(strconv.Itoa(int(svcPort.Port)), usedRemotePorts)
			// Skip generating rules if any of values are not available
			if source.SourceIP == "" || remoteFwdPort == "" || clusterIP == "" || strconv.Itoa(int(svcPort.Port)) == "" {
				continue
			}
			rules += fmt.Sprintf("%s:%s:%s:%s,%s\n", source.SourceIP, remoteFwdPort, clusterIP, strconv.Itoa(int(svcPort.Port)), source.SourceIP)
		}
	}

	return rules
}

func (r *ReconcileExternalService) getUsedRemotePorts(sourceIP string) (map[string]string, error) {
	usedRemotePorts := map[string]string{}
	// TODO: add prefix to configmapName
	configmapName := sourceIP
	configMap := &corev1.ConfigMap{}
	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: configmapName, Namespace: ConnectorNamespace}, configMap); err != nil && !errors.IsNotFound(err) {
		return usedRemotePorts, err
	}

	if rules, ok := configMap.Data["iptables-rule"]; ok {
		for _, s := range strings.Split(string(rules), "\n") {
			fields := strings.Fields(s)
			if len(fields) > 16 && fields[0] == "PREROUTING" {
				ipPort := strings.Split(fields[16], ":")
				if len(ipPort) > 1 {
					// Reference for port to forward port
					usedRemotePorts[fields[13]] = ipPort[1]
					// Reference for forward port to port
					usedRemotePorts[ipPort[1]] = fields[13]
				}
			}
		}
	}

	return usedRemotePorts, nil
}

func genRemotePort(port string, usedRemotePorts map[string]string) string {
	if val, ok := usedRemotePorts[port]; ok {
		return val
	}
	for fwdPort := MinPort; fwdPort < MaxPort+1; fwdPort++ {
		strFwdPort := strconv.Itoa(fwdPort)
		if _, ok := usedRemotePorts[strFwdPort]; !ok {
			// Reference for port to forward port
			usedRemotePorts[port] = strFwdPort
			// Reference for forward port to port
			usedRemotePorts[strFwdPort] = port
			return strFwdPort
		}
	}

	return ""
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
			fwdPort := getPort(source.SourceIP, port.TargetPort.String(), usedPorts)
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

func genPort(sourceIP string, targetPort string, usedPorts map[string]string) string {

	for port := MinPort; port < MaxPort+1; port++ {
		strPort := strconv.Itoa(port)
		if _, ok := usedPorts[strPort]; !ok {
			usedPorts[strPort] = sourceIP + ":" + targetPort
			return strPort
		}
	}

	return ""
}

func getPort(sourceIP string, targetPort string, usedPorts map[string]string) string {
	for port, usedBy := range usedPorts {
		if usedBy == sourceIP+":"+targetPort {
			return port
		}
	}

	return ""
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

// TODO: move getHexIP and getRuleName to util
func getHexIP(ip string) (string, error) {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return "", fmt.Errorf("getHexIP: failed to parse ip %q", ip)
	}
	v4IP := parsedIP.To4()
	if v4IP == nil {
		return "", fmt.Errorf("getHexIP: failed to convert ip %v to 4 bytes", parsedIP)
	}

	return fmt.Sprintf("%02x%02x%02x%02x", v4IP[0], v4IP[1], v4IP[2], v4IP[3]), nil
}

const gatewayRulePrefix = "gwrule"

func getRuleName(ip string) (string, error) {
	hexIP, err := getHexIP(ip)
	if err != nil {
		return "", err
	}

	return gatewayRulePrefix + hexIP, nil
}
