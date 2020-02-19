package externalservice

import (
	"context"

	submarinerv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
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
						Name: "external-service-data",
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
