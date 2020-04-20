package gateway

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	backoffv4 "github.com/cenkalti/backoff/v4"
	"github.com/golang/glog"
	"github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	clv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/client/clientset/versioned/typed/submariner/v1alpha1"
	"github.com/mkimuram/k8s-ext-connector/pkg/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Gateway represents all information to configure a gateway
type Gateway struct {
	clientset *clv1alpha1.SubmarinerV1alpha1Client
	namespace string
	ssh       map[string]context.CancelFunc
}

// NewGateway returns an Gateway instance
func NewGateway(clientset *clv1alpha1.SubmarinerV1alpha1Client, namespace string) *Gateway {
	return &Gateway{
		clientset: clientset,
		namespace: namespace,
		ssh:       map[string]context.CancelFunc{},
	}
}

func needSync(gw *v1alpha1.Gateway) bool {
	// Sync is needed if
	// - generations are different between rule and sync &&
	// - rule is not updating
	return gw.Status.RuleGeneration != gw.Status.SyncGeneration &&
		gw.Status.Conditions.IsFalseFor(v1alpha1.ConditionRuleUpdating)
}

func setSyncing(clientset *clv1alpha1.SubmarinerV1alpha1Client, ns string, gw *v1alpha1.Gateway) error {
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

func setSynced(clientset *clv1alpha1.SubmarinerV1alpha1Client, ns string, gw *v1alpha1.Gateway) error {
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

// Reconcile reconciles the gateway configuration to the desired state
func (g *Gateway) Reconcile() {
	opts := metav1.ListOptions{}
	watch, err := g.clientset.Gateways(g.namespace).Watch(opts)
	if err != nil {
		panic(err.Error())
	}
	for event := range watch.ResultChan() {
		glog.Errorf("Type: %v", event.Type)
		gw, ok := event.Object.(*v1alpha1.Gateway)
		if !ok {
			glog.Errorf("Not a gateway: %v", event.Object)
			continue
		}
		if needSync(gw) {
			if err := setSyncing(g.clientset, g.namespace, gw); err != nil {
				// TODO: requeue the event
				continue
			}

			if err := g.SyncRule(gw); err != nil {
				// TODO: requeue the event
				continue
			}

			if err := setSynced(g.clientset, g.namespace, gw); err != nil {
				// TODO: requeue the event
				continue
			}
		}
	}
}

func (g *Gateway) SyncRule(gw *v1alpha1.Gateway) error {
	if err := g.ensureSshdRunning(gw.Spec.GatewayIP); err != nil {
		return err
	}
	// Apply iptables rules for gw
	if err := g.applyIptablesRules(gw); err != nil {
		return err
	}

	return nil
}

func (g *Gateway) ensureSshdRunning(ip string) error {
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

func getExpectedIptablesRule(gw *v1alpha1.Gateway) []string {
	it := []string{}
	// Format gw.Spec.IngressRules to
	//   PREROUTING -t nat -m tcp -p tcp --dst {GatewayIP} --src {SourceIP} --dport {TargetPort} -j DNAT --to-destination {GatewayIp}:{RelayPort}
	//   POSTROUTING -t nat -m tcp -p tcp --dst {DestinationIP} --dport {RelayPort} -j SNAT --to-source {GatewayIP}
	// ex)
	//    PREROUTING -t nat -m tcp -p tcp --dst 192.168.122.200 --src 192.168.122.140 --dport 80 -j DNAT --to-destination 192.168.122.200:2049
	//    POSTROUTING -t nat -m tcp -p tcp --dst 192.168.122.140 --dport 2049 -j SNAT --to-source 192.168.122.200
	// TODO: Also handle UDP properly
	for _, rule := range gw.Spec.EgressRules {
		it = append(it, fmt.Sprintf("PREROUTING -t nat -m tcp -p tcp --dst %s --src %s --dport %s -j DNAT --to-destination %s:%s\n", gw.Spec.GatewayIP, rule.SourceIP, rule.TargetPort, gw.Spec.GatewayIP, rule.RelayPort))
		it = append(it, fmt.Sprintf("POSTROUTING -t nat -m tcp -p tcp --dst %s --dport %s -j SNAT --to-source %s\n", rule.DestinationIP, rule.RelayPort, gw.Spec.GatewayIP))
	}

	return it
}

func (g *Gateway) applyIptablesRules(gw *v1alpha1.Gateway) error {
	// TODO: Clear unused existing iptables rules
	// Apply all iptables rules
	errStr := ""
	for _, rule := range getExpectedIptablesRule(gw) {
		args := []string{"-A"}
		ruleStrs := strings.Fields(rule)
		if len(ruleStrs) == 0 {
			// Skip empty rule
			continue
		}
		args = append(args, ruleStrs...)
		cmd := exec.Command("iptables", args...)
		if err := cmd.Run(); err != nil {
			// Append error and continue
			errStr = errStr + fmt.Sprintf("failed to apply iptables rule %q: %v, ", rule, err)
		}
	}

	if errStr != "" {
		return fmt.Errorf("applyIptablesRules: %q", errStr)
	}

	return nil
}
