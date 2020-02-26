package externalservice

import (
	"context"
	"fmt"
	"strconv"

	submarinerv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	"github.com/mkimuram/k8s-ext-connector/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_externalservice")

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

	// Watch for changes to secondary resource Pods and requeue the owner ExternalService
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &submarinerv1alpha1.ExternalService{},
	})
	if err != nil {
		return err
	}

	// Watch for changes to secondary resource Services and requeue the owner ExternalService
	err = c.Watch(&source.Kind{Type: &corev1.Service{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &submarinerv1alpha1.ExternalService{},
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

	// Define a new forwarder Pod object
	pod := genForwardPodSpec(instance)

	// Set ExternalService instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, pod, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Check if this Pod already exists
	foundPod := &corev1.Pod{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, foundPod)
	if err != nil && !errors.IsNotFound(err) {
		return reconcile.Result{}, err
	} else if err != nil {
		// Ensure configmap exists before creating forwarder pod
		configMap, err := util.GetOrCreateConfigMap(r.client, instance.Name, instance.Namespace)
		if err != nil {
			return reconcile.Result{}, err
		}
		// Set ExternalService instance as the owner and controller
		if err := controllerutil.SetControllerReference(instance, configMap, r.scheme); err != nil {
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

	// Set ExternalService instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, service, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

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

	// Update configmap
	err = r.updateConfigmapDataForCR(instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// genForwardPodSpec returns a spec for a forwarder pod
func genForwardPodSpec(cr *submarinerv1alpha1.ExternalService) *corev1.Pod {
	labels := map[string]string{
		"externalService": cr.Name,
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
			Namespace: cr.Namespace,
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
		"externalService": cr.Name,
	}

	for _, port := range cr.Spec.Ports {
		ports = append(ports, port)
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: cr.Namespace,
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
	config, err := util.GetOrCreateConfigMap(r.client, cr.Name, cr.Namespace)
	if err != nil {
		return err
	}

	// Update data
	usedPort := map[string]string{}
	//usedRemotePort := map[string]string{}
	data := r.genSSHTunnelRules(cr, usedPort)
	//data += genRemoteSSHTunnelRules(cr, usedRemotePort)
	data += r.genIptablesRules(cr, usedPort)
	configmapData := map[string]string{"data.yaml": data}

	// Update configmap with the data
	if err := util.UpdateConfigmapData(r.client, config, configmapData); err != nil {
		return err
	}

	return nil
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

func (r *ReconcileExternalService) genRemoteSSHTunnelRules(cr *submarinerv1alpha1.ExternalService, usedRemotePorts map[string]string) string {
	rules := ""

	for _, source := range cr.Spec.Sources {
		svc := &corev1.Service{}
		err := r.client.Get(context.TODO(), types.NamespacedName{Name: source.Service.Name, Namespace: source.Service.Namespace}, svc)
		if err != nil && !errors.IsNotFound(err) {
			// TODO: Handle error properly
			continue
		}
		clusterIP := svc.Spec.ClusterIP
		for _, svcPort := range svc.Spec.Ports {
			//remoteFwdPort := genRemotePort(source.SourceIP, clusterIP, svcPort.Port, usedRemotePorts)
			// TODO: implement genRemotePort
			remoteFwdPort := ""
			// Skip generating rules if any of values are not available
			if source.SourceIP == "" || remoteFwdPort == "" || clusterIP == "" || strconv.Itoa(int(svcPort.Port)) == "" {
				continue
			}
			rules += fmt.Sprintf("%s:%s:%s:%s,%s\n", source.SourceIP, remoteFwdPort, clusterIP, strconv.Itoa(int(svcPort.Port)), source.SourceIP)
		}
	}

	return rules
}

func (r *ReconcileExternalService) genIptablesRules(cr *submarinerv1alpha1.ExternalService, usedPorts map[string]string) string {
	logger := log.WithValues("Namespace", cr.Namespace, "Name", cr.Name)
	logger.Info("genIptablesRules")

	rules := ""

	// TODO: get fwdPodIP properly
	fwdPod := &corev1.Pod{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}, fwdPod)
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
	MINPORT := 2049
	MAXPORT := 65536

	for port := MINPORT; port < MAXPORT+1; port++ {
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
