package gateway

import (
	"context"
	"time"

	backoffv4 "github.com/cenkalti/backoff/v4"
	glssh "github.com/gliderlabs/ssh"
	"github.com/golang/glog"
	"github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	clv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/client/clientset/versioned/typed/submariner/v1alpha1"
	"github.com/mkimuram/k8s-ext-connector/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	prechainPrefix  = "pre"
	postchainPrefix = "pst"
)

// Reconciler represents a reconciler for gateway
type Reconciler struct {
	clientset clv1alpha1.SubmarinerV1alpha1Interface
	namespace string
	ssh       map[string]glssh.Server
}

var _ util.ReconcilerInterface = &Reconciler{}

// NewReconciler returns a Reconciler instance
func NewReconciler(cl clv1alpha1.SubmarinerV1alpha1Interface, ns string) *Reconciler {
	return &Reconciler{
		clientset: cl,
		namespace: ns,
		ssh:       map[string]glssh.Server{},
	}
}

// Reconcile reconciles gateway
func (g *Reconciler) Reconcile(namespace, name string) error {
	// Check if the resource is in namespace to be handled
	if g.namespace != namespace {
		// no need to handle this resource
		return nil
	}

	gw, err := g.clientset.Gateways(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		return err
	}
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

func (g *Reconciler) syncRule(gw *v1alpha1.Gateway) error {
	if err := g.ensureSshdRunning(gw.Spec.GatewayIP); err != nil {
		return err
	}
	// Apply iptables rules for gw
	if err := g.applyIptablesRules(gw); err != nil {
		return err
	}

	return nil
}

func (g *Reconciler) ensureSshdRunning(ip string) error {
	if _, ok := g.ssh[ip]; ok {
		// Already running, skip creating new server
		return nil
	}

	srv := util.NewSSHServer(ip + ":" + util.SSHPort)
	b := backoffv4.WithContext(backoffv4.NewExponentialBackOff(), context.Background())
	go backoffv4.RetryNotify(
		func() error {
			if err := srv.ListenAndServe(); err != nil {
				return err
			}
			return nil
		},
		b,
		func(err error, tm time.Duration) {
			glog.Errorf("error in sshd for %q in duration %v: %v", ip, tm, err)
		},
	)

	g.ssh[ip] = srv

	return nil
}

// TODO: check that this works well
func (g *Reconciler) stopSshd(ip string) error {
	srv, ok := g.ssh[ip]
	if !ok {
		// Already stopped
		return nil
	}

	if err := srv.Close(); err != nil {
		return err
	}
	delete(g.ssh, ip)

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

func (g *Reconciler) applyIptablesRules(gw *v1alpha1.Gateway) error {
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

func (g *Reconciler) ruleSynced(gw *v1alpha1.Gateway) bool {
	return g.checkSshdRunning(gw.Spec.GatewayIP) && g.checkIptablesRulesApplied(gw)
}

func (g *Reconciler) checkSshdRunning(ip string) bool {
	// TODO: consider more strict check?
	// below only check that the port is open in the specified ip
	return util.IsPortOpen(ip, util.SSHPort)
}

func (g *Reconciler) checkIptablesRulesApplied(gw *v1alpha1.Gateway) bool {
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
