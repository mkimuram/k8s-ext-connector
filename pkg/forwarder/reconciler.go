package forwarder

import (
	"fmt"
	"strings"

	"github.com/golang/glog"
	"github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	clv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/client/clientset/versioned/typed/submariner/v1alpha1"
	"github.com/mkimuram/k8s-ext-connector/pkg/util"
	"golang.org/x/crypto/ssh"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Reconciler represents a reconciler for forwarder
type Reconciler struct {
	clientset     clv1alpha1.SubmarinerV1alpha1Interface
	namespace     string
	name          string
	tunnels       map[string]*util.Tunnel
	remoteTunnels map[string]*util.Tunnel
	config        *ssh.ClientConfig
}

var _ util.ReconcilerInterface = &Reconciler{}

// NewReconciler returns a Reconciler instance
func NewReconciler(cl clv1alpha1.SubmarinerV1alpha1Interface, namespace, name string) *Reconciler {
	// TODO: Create clientconfig properly
	user := "root"
	password := "password"

	return &Reconciler{
		clientset:     cl,
		namespace:     namespace,
		name:          name,
		tunnels:       map[string]*util.Tunnel{},
		remoteTunnels: map[string]*util.Tunnel{},
		config: &ssh.ClientConfig{
			User: user,
			Auth: []ssh.AuthMethod{
				ssh.Password(password),
			},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		},
	}
}

// Reconcile reconciles forwarder
func (f *Reconciler) Reconcile(namespace, name string) error {
	// Check if the resource needs to be handled
	if f.namespace != namespace || f.name != name {
		// no need to handle this resource
		return nil
	}
	fwd, err := f.clientset.Forwarders(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if needSync(fwd) {
		if err := setSyncing(f.clientset, namespace, fwd); err != nil {
			return err
		}

		if err := f.syncRule(fwd); err != nil {
			glog.Errorf("failed to sync rule: %v", err)
			return err
		}

		if err := setSynced(f.clientset, namespace, fwd); err != nil {
			return err
		}
	} else if needCheckSync(fwd) {
		if !f.ruleSynced(fwd) {
			glog.Errorf("rule for %s/%s is not synced any more", namespace, name)
			// Set to syncing and return error to requeue and sync
			if err := setSyncing(f.clientset, namespace, fwd); err != nil {
				return err
			}
		}
	}

	return nil
}

func (f *Reconciler) syncRule(fwd *v1alpha1.Forwarder) error {
	f.updateSSHTunnel(getExpectedSSHTunnel(fwd))
	f.updateRemoteSSHTunnel(getExpectedRemoteSSHTunnel(fwd))

	if err := updateIptablesRule(getExpectedIptablesRule(fwd)); err != nil {
		glog.Errorf("failed to update iptables rule: %v", err)
		return err
	}

	return nil
}

func (f *Reconciler) toTunnel(tun string) *util.Tunnel {
	s := strings.Split(tun, ":")
	local := fmt.Sprintf("%s:%s", s[0], s[1])
	server := fmt.Sprintf("%s:%s", s[2], s[3])
	remote := fmt.Sprintf("%s:%s", s[4], s[5])

	return util.NewTunnel(local, server, remote, f.config)
}

func (f *Reconciler) deleteUnusedSSHTunnel(expected map[string]bool) {
	deleted := []string{}
	for k, tunnel := range f.tunnels {
		if _, ok := expected[k]; !ok {
			glog.Infof("delete ssh tunnel for: %v", k)
			tunnel.Cancel()
			deleted = append(deleted, k)
		}
	}

	for _, d := range deleted {
		delete(f.tunnels, d)
	}
}

func (f *Reconciler) ensureSSHTunnel(expected map[string]bool) {
	created := map[string]*util.Tunnel{}
	for k := range expected {
		if _, ok := f.tunnels[k]; ok {
			// Already exists, skip creating tunnel
			continue
		}
		glog.Infof("create new ssh tunnel for: %v", k)
		tunnel := f.toTunnel(k)
		tunnel.ForwardNB()

		created[k] = tunnel
	}

	for k, v := range created {
		f.tunnels[k] = v
	}
}

func (f *Reconciler) deleteUnusedRemoteSSHTunnel(expected map[string]bool) {
	deleted := []string{}
	for k, tunnel := range f.remoteTunnels {
		if _, ok := expected[k]; !ok {
			glog.Infof("delete remote ssh tunnel for: %v", k)
			tunnel.Cancel()
			deleted = append(deleted, k)
		}
	}

	for _, d := range deleted {
		delete(f.remoteTunnels, d)
	}
}

func (f *Reconciler) ensureRemoteSSHTunnel(expected map[string]bool) {
	created := map[string]*util.Tunnel{}
	for k := range expected {
		if _, ok := f.remoteTunnels[k]; ok {
			// Already exists, skip creating tunnel
			continue
		}
		glog.Infof("create new remote ssh tunnel for: %v", k)
		tunnel := f.toTunnel(k)
		tunnel.RemoteForwardNB()

		created[k] = tunnel
	}

	for k, v := range created {
		f.remoteTunnels[k] = v
	}
}

func (f *Reconciler) updateSSHTunnel(expected map[string]bool) {
	f.deleteUnusedSSHTunnel(expected)
	f.ensureSSHTunnel(expected)
}

func (f *Reconciler) updateRemoteSSHTunnel(expected map[string]bool) {
	f.deleteUnusedRemoteSSHTunnel(expected)
	f.ensureRemoteSSHTunnel(expected)
}

func updateIptablesRule(expected map[string][][]string) error {
	return util.ReplaceChains(util.TableNAT, expected)
}

func getExpectedSSHTunnel(fwd *v1alpha1.Forwarder) map[string]bool {
	st := map[string]bool{}
	// Format fwd.Spec.EgressRules to
	// {ForwarderIP}:{RelayPort}:{GatewayIP}:22:{DestinationIp}:{DestinationPort}
	// TODO: make 22 a variable
	// ex)
	//   "10.0.0.2:2049:192.168.122.201:22:192.168.122.140:8000"
	for _, rule := range fwd.Spec.EgressRules {
		st[fmt.Sprintf("%s:%s:%s:%s:%s:%s", fwd.Spec.ForwarderIP, rule.RelayPort, rule.GatewayIP, util.SSHPort, rule.DestinationIP, rule.DestinationPort)] = true
	}

	return st
}

func getExpectedRemoteSSHTunnel(fwd *v1alpha1.Forwarder) map[string]bool {
	rt := map[string]bool{}
	// Format fwd.Spec.IngressRules to
	// {DestinationIp}:{DestinationPort}:{GatewayIP}:22:{GatewayIP}:{RelayPort}
	// TODO: make 22 a variable
	// ex)
	//   "192.168.122.201:2049:192.168.122.201:22:10.96.218.78:80"
	for _, rule := range fwd.Spec.IngressRules {
		rt[fmt.Sprintf("%s:%s:%s:%s:%s:%s", rule.DestinationIP, rule.DestinationPort, rule.GatewayIP, util.SSHPort, rule.GatewayIP, rule.RelayPort)] = true
	}

	return rt
}

func getExpectedIptablesRule(fwd *v1alpha1.Forwarder) map[string][][]string {
	it := map[string][][]string{util.ChainPrerouting: [][]string{}, util.ChainPostrouting: [][]string{}}
	// Format fwd.Spec.EgressRules to
	//   PREROUTING:
	//     -m tcp -p tcp --dst {ForwarderIP} --src {SourceIP} --dport {TargetPort} -j DNAT --to-destination {ForwarderIp}:{RelayPort}
	//   POSTROUTING:
	//     -m tcp -p tcp --dst {DestinationIP} --dport {RelayPort} -j SNAT --to-source {ForwarderIP}
	// ex)
	//   PREROUTING:
	//     "-m tcp -p tcp --dst 10.244.0.34 --src 10.244.0.11 --dport 8000 -j DNAT --to-destination 10.244.0.34:2049"
	//   POSTROUTING:
	//     "-m tcp -p tcp --dst 192.168.122.139 --dport 2049 -j SNAT --to-source 10.244.0.34"
	// TODO: Also handle UDP properly
	for _, rule := range fwd.Spec.EgressRules {
		it[util.ChainPrerouting] = append(it[util.ChainPrerouting], util.DNATRuleSpec(fwd.Spec.ForwarderIP, rule.SourceIP, rule.TargetPort, fwd.Spec.ForwarderIP, rule.RelayPort))
		it[util.ChainPostrouting] = append(it[util.ChainPostrouting], util.SNATRuleSpec(rule.DestinationIP, fwd.Spec.ForwarderIP, rule.RelayPort))
	}

	return it
}

func (f *Reconciler) ruleSynced(fwd *v1alpha1.Forwarder) bool {
	return f.isTunnelRunning(fwd) && f.isIptablesRulesApplied(fwd)
}

func (f *Reconciler) isTunnelRunning(fwd *v1alpha1.Forwarder) bool {
	for k := range getExpectedSSHTunnel(fwd) {
		if _, ok := f.tunnels[k]; !ok {
			return false
		}
	}
	for k := range getExpectedRemoteSSHTunnel(fwd) {
		if _, ok := f.remoteTunnels[k]; !ok {
			return false
		}
	}
	// TODO: consider checking actual open ports like below
	/*
		for _, rule := range fwd.Spec.EgressRules {
			if !util.IsPortOpen(fwd.Spec.ForwarderIP, rule.RelayPort) {
				return false
			}
		}
		for _, rule := range fwd.Spec.IngressRules {
			if !util.IsPortOpen(rule.GatewayIP, rule.RelayPort) {
				return false
			}
		}
	*/
	return true
}

func (f *Reconciler) isIptablesRulesApplied(fwd *v1alpha1.Forwarder) bool {
	// TODO: consider checking exact match?
	// below only check that rules in chains do exist, so unused rules might remain
	return util.CheckChainsExist(util.TableNAT, getExpectedIptablesRule(fwd))
}
