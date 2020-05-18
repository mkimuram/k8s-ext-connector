package externalservice

import (
	"context"

	submarinerv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
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
