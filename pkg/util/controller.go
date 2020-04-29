package util

import (
	"fmt"

	"github.com/golang/glog"
	clv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/client/clientset/versioned/typed/submariner/v1alpha1"
	sbinformers "github.com/mkimuram/k8s-ext-connector/pkg/client/informers/externalversions"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

// ReconcilerInterface is an interface for reconciler
type ReconcilerInterface interface {
	Reconcile(namespace, name string) error
}

// Controller represents a cotroller
type Controller struct {
	clientset  clv1alpha1.SubmarinerV1alpha1Interface
	informer   cache.SharedIndexInformer
	workqueue  workqueue.RateLimitingInterface
	reconciler ReconcilerInterface
}

// NewController returns a controller instance
func NewController(cl clv1alpha1.SubmarinerV1alpha1Interface, informerFactory sbinformers.SharedInformerFactory, informer cache.SharedIndexInformer, reconciler ReconcilerInterface) *Controller {
	wq := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	controller := &Controller{
		clientset:  cl,
		informer:   informer,
		workqueue:  wq,
		reconciler: reconciler,
	}

	controller.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(object interface{}) {
			controller.enqueue(object)
		},
		UpdateFunc: func(oldObject, newObject interface{}) {
			controller.enqueue(newObject)
		},
		DeleteFunc: func(object interface{}) {
			controller.enqueue(object)
		},
	})

	informerFactory.Start(wait.NeverStop)

	return controller
}

func (c *Controller) enqueue(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.workqueue.Add(key)
}

func getKey(obj interface{}) string {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		return ""
	}
	return key
}

// Run runs a controller
func (c *Controller) Run() {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()

	if ok := cache.WaitForCacheSync(wait.NeverStop, c.informer.HasSynced); !ok {
		glog.Fatalf("time out while waiting cache to be synced")
	}

	c.reconcile()
}

// reconcile reconciles the connfiguration to the desired state
func (c *Controller) reconcile() {
	for c.processNext() {
	}
}

func (c *Controller) processNext() bool {
	obj, shutdown := c.workqueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.workqueue.Done(obj)
		key, ok := obj.(string)
		if !ok {
			c.workqueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("invalid key is passed to workqueue"))
			return nil
		}
		namespace, name, err := cache.SplitMetaNamespaceKey(key)
		if err != nil {
			return err
		}

		if err := c.reconciler.Reconcile(namespace, name); err != nil {
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing %q: %v", key, err)
		}
		c.workqueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}
