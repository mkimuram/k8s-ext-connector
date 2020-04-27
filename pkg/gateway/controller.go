package gateway

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	clversioned "github.com/mkimuram/k8s-ext-connector/pkg/client/clientset/versioned"
	clv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/client/clientset/versioned/typed/submariner/v1alpha1"
	sbinformers "github.com/mkimuram/k8s-ext-connector/pkg/client/informers/externalversions"
	sblisters "github.com/mkimuram/k8s-ext-connector/pkg/client/listers/submariner/v1alpha1"
	"github.com/mkimuram/k8s-ext-connector/pkg/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
)

// GatewayController is a cotroller for a gateway
type GatewayController struct {
	clientset clv1alpha1.SubmarinerV1alpha1Interface
	namespace string
	informer  cache.SharedIndexInformer
	lister    sblisters.GatewayLister
	workqueue workqueue.RateLimitingInterface
	syncer    GatewaySyncerInterface
}

// NewGatewayController returns a GatewayController instance
func NewGatewayController(cl clv1alpha1.SubmarinerV1alpha1Interface, vcl clversioned.Interface, namespace string, syncer GatewaySyncerInterface) *GatewayController {
	wq := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	informerFactory := sbinformers.NewSharedInformerFactory(vcl, time.Second*30)
	informer := informerFactory.Submariner().V1alpha1().Gateways()
	controller := &GatewayController{
		clientset: cl,
		namespace: namespace,
		informer:  informer.Informer(),
		lister:    informer.Lister(),
		workqueue: wq,
		syncer:    syncer,
	}

	controller.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(object interface{}) {
			if !controller.needEnqueue(object) {
				return
			}
			klog.Infof("Added: %s", getKey(object))
			controller.enqueueGateway(object)
		},
		UpdateFunc: func(oldObject, newObject interface{}) {
			if !controller.needEnqueue(newObject) {
				return
			}
			klog.Infof("Updated: %s", getKey(newObject))
			controller.enqueueGateway(newObject)
		},
		DeleteFunc: func(object interface{}) {
			if !controller.needEnqueue(object) {
				return
			}
			klog.Infof("Deleted: %v", getKey(object))
			controller.enqueueGateway(object)
		},
	})

	informerFactory.Start(wait.NeverStop)

	return controller
}

func (g *GatewayController) needEnqueue(obj interface{}) bool {
	if _, ok := obj.(*v1alpha1.Gateway); !ok {
		return false
	}

	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		return false
	}

	namespace, _, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return false
	}

	return g.namespace == namespace
}

func (g *GatewayController) enqueueGateway(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	g.workqueue.Add(key)
}

func getKey(obj interface{}) string {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		return ""
	}
	return key
}

func (g *GatewayController) Run() {
	defer utilruntime.HandleCrash()
	defer g.workqueue.ShutDown()

	if ok := cache.WaitForCacheSync(wait.NeverStop, g.informer.HasSynced); !ok {
		glog.Fatalf("time out while waiting cache to be synced")
	}

	g.reconcile()
}

// reconcile reconciles the gateway configuration to the desired state
func (g *GatewayController) reconcile() {
	for g.processNextGateway() {
	}
}

func (g *GatewayController) processNextGateway() bool {
	obj, shutdown := g.workqueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer g.workqueue.Done(obj)
		key, ok := obj.(string)
		if !ok {
			g.workqueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("invalid key is passed to workqueue"))
			return nil
		}

		if err := g.syncGateway(key); err != nil {
			g.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing %q: %v", key, err)
		}
		g.workqueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

func (g *GatewayController) syncGateway(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	gw, err := g.clientset.Gateways(namespace).Get(name, metav1.GetOptions{})
	if needSync(gw) {
		if err := setSyncing(g.clientset, namespace, gw); err != nil {
			return err
		}

		if err := g.syncer.syncRule(gw); err != nil {
			glog.Errorf("failed to sync rule for %s/%s: %v", namespace, name, err)
			return err
		}

		if err := setSynced(g.clientset, namespace, gw); err != nil {
			return err
		}
	} else if needCheckSync(gw) {
		if !g.syncer.ruleSynced(gw) {
			glog.Errorf("rule for %s/%s is not synced any more", namespace, name)
			// Set to syncing and return error to requeue and sync
			if err := setSyncing(g.clientset, namespace, gw); err != nil {
				return err
			}
		}
	}

	return nil
}

func needSync(gw *v1alpha1.Gateway) bool {
	// Sync is needed if
	// - rule is not updating
	// - generations are different between rule and sync || not is not synced
	return gw.Status.Conditions.IsFalseFor(v1alpha1.ConditionRuleUpdating) &&
		(gw.Status.RuleGeneration != gw.Status.SyncGeneration ||
			gw.Status.Conditions.IsTrueFor(v1alpha1.ConditionRuleSyncing))
}

func needCheckSync(gw *v1alpha1.Gateway) bool {
	// CheckSync is needed if
	// - rule is not updating
	// - generations are the same between rule and sync
	// - rule is synced
	return gw.Status.Conditions.IsFalseFor(v1alpha1.ConditionRuleUpdating) &&
		gw.Status.RuleGeneration == gw.Status.SyncGeneration &&
		gw.Status.Conditions.IsFalseFor(v1alpha1.ConditionRuleSyncing)
}

func setSyncing(clientset clv1alpha1.SubmarinerV1alpha1Interface, ns string, gw *v1alpha1.Gateway) error {
	var err error
	if gw.Status.Conditions.SetCondition(util.RuleSyncingCondition(corev1.ConditionTrue)) {
		gw, err = clientset.Gateways(ns).UpdateStatus(gw)
		if err != nil {
			return err
		}
		glog.Infof("Update RuleSyncingCondition to true")
	}
	return nil
}

func setSynced(clientset clv1alpha1.SubmarinerV1alpha1Interface, ns string, gw *v1alpha1.Gateway) error {
	var err error
	if gw.Status.Conditions.SetCondition(util.RuleSyncingCondition(corev1.ConditionFalse)) {
		gw.Status.SyncGeneration = gw.Status.RuleGeneration
		gw, err = clientset.Gateways(ns).UpdateStatus(gw)
		if err != nil {
			return err
		}
		glog.Infof("Update RuleSyncingCondition to false")
	}
	return nil
}
