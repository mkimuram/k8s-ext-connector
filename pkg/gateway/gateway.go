package gateway

import (
	"context"
	"fmt"
	"time"

	backoffv4 "github.com/cenkalti/backoff/v4"
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

const (
	prechainPrefix  = "pre"
	postchainPrefix = "pst"
)

// GatewayController is a cotroller for a gateway
type GatewayController struct {
	clientset *clv1alpha1.SubmarinerV1alpha1Client
	namespace string
	ssh       map[string]context.CancelFunc
	informer  cache.SharedIndexInformer
	lister    sblisters.GatewayLister
	workqueue workqueue.RateLimitingInterface
}

// NewGatewayController returns a GatewayController instance
func NewGatewayController(cl *clv1alpha1.SubmarinerV1alpha1Client, vcl *clversioned.Clientset, namespace string) *GatewayController {
	wq := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	informerFactory := sbinformers.NewSharedInformerFactory(vcl, time.Second*30)
	informer := informerFactory.Submariner().V1alpha1().Gateways()
	controller := &GatewayController{
		clientset: cl,
		namespace: namespace,
		ssh:       map[string]context.CancelFunc{},
		informer:  informer.Informer(),
		lister:    informer.Lister(),
		workqueue: wq,
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

		if err := g.syncRule(gw); err != nil {
			glog.Errorf("failed to sync rule for %s/%s: %v", namespace, name, err)
			return err
		}

		if err := setSynced(g.clientset, namespace, gw); err != nil {
			return err
		}
	} else if needCheckSync(gw) {
		if !g.ruleSynced(gw) {
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

func (g *GatewayController) syncRule(gw *v1alpha1.Gateway) error {
	if err := g.ensureSshdRunning(gw.Spec.GatewayIP); err != nil {
		return err
	}
	// Apply iptables rules for gw
	if err := g.applyIptablesRules(gw); err != nil {
		return err
	}

	return nil
}

func (g *GatewayController) ensureSshdRunning(ip string) error {
	if _, ok := g.ssh[ip]; ok {
		// Already running, skip creating new server
		return nil
	}

	srv := util.NewSSHServer(ip + ":" + util.SSHPort)
	ctx, cf := context.WithCancel(context.Background())
	b := backoffv4.WithContext(backoffv4.NewExponentialBackOff(), ctx)
	go backoffv4.Retry(
		func() error {
			select {
			case <-ctx.Done():
				// TODO: Consider handling error for Close
				srv.Close()
				return nil
			default:
				if err := srv.ListenAndServe(); err != nil {
					return err
				}
			}
			return nil
		},
		b)

	g.ssh[ip] = cf

	return nil
}

func getExpectedIptablesRule(gw *v1alpha1.Gateway) (map[string][][]string, map[string][][]string, error) {
	hexIP, err := util.GetHexIP(gw.Spec.GatewayIP)
	if err != nil {
		return nil, nil, err
	}

	preChain := prechainPrefix + hexIP
	postChain := postchainPrefix + hexIP
	jumpChains := map[string][][]string{
		util.ChainPrerouting:  [][]string{[]string{"-j", preChain}},
		util.ChainPostrouting: [][]string{[]string{"-j", postChain}},
	}
	chains := map[string][][]string{
		preChain:  [][]string{},
		postChain: [][]string{},
	}
	// Format gw.Spec.IngressRules to
	//   PREROUTING:
	//     -m tcp -p tcp --dst {GatewayIP} --src {SourceIP} --dport {TargetPort} -j DNAT --to-destination {GatewayIp}:{RelayPort}
	//   POSTROUTING:
	//     -m tcp -p tcp --dst {DestinationIP} --dport {RelayPort} -j SNAT --to-source {GatewayIP}
	// ex)
	//   PREROUTING:
	//     -m tcp -p tcp --dst 192.168.122.200 --src 192.168.122.140 --dport 80 -j DNAT --to-destination 192.168.122.200:2049
	//   POSTROUTING:
	//     -m tcp -p tcp --dst 192.168.122.140 --dport 2049 -j SNAT --to-source 192.168.122.200
	// TODO: Also handle UDP properly
	for _, rule := range gw.Spec.IngressRules {
		chains[preChain] = append(chains[preChain], util.DNATRuleSpec(gw.Spec.GatewayIP, rule.SourceIP, rule.TargetPort, gw.Spec.GatewayIP, rule.RelayPort))
		chains[postChain] = append(chains[postChain], util.SNATRuleSpec(rule.DestinationIP, gw.Spec.GatewayIP, rule.RelayPort))
	}

	return jumpChains, chains, nil
}

func (g *GatewayController) applyIptablesRules(gw *v1alpha1.Gateway) error {
	jumpChains, chains, err := getExpectedIptablesRule(gw)
	if err != nil {
		return err
	}

	if err := util.ReplaceChains(util.TableNAT, chains); err != nil {
		return err
	}

	if err := util.AddChains(util.TableNAT, jumpChains); err != nil {
		return err
	}

	return nil
}

func (g *GatewayController) ruleSynced(gw *v1alpha1.Gateway) bool {
	return g.checkSshdRunning(gw.Spec.GatewayIP) && g.checkIptablesRulesApplied(gw)
}

func (g *GatewayController) checkSshdRunning(ip string) bool {
	// TODO: consider more strict check?
	// below only check that the port is open in the specified ip
	return util.IsPortOpen(ip, util.SSHPort)
}

func (g *GatewayController) checkIptablesRulesApplied(gw *v1alpha1.Gateway) bool {
	jumpChains, chains, err := getExpectedIptablesRule(gw)
	if err != nil {
		return false
	}
	// TODO: consider checking exact match?
	// below only check that rules in chains do exist, so unused rules might remain
	if !util.CheckChainsExist(util.TableNAT, chains) {
		return false
	}
	if !util.CheckChainsExist(util.TableNAT, jumpChains) {
		return false
	}
	return true
}
