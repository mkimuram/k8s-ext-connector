package gateway

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/golang/glog"
	"github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	clv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/client/clientset/versioned/typed/submariner/v1alpha1"
	"github.com/mkimuram/k8s-ext-connector/pkg/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// sshdCommand is a path to sshd command (TODO: Need to make this variable or auto detect?)
	sshdCommand = "/usr/sbin/sshd"
)

// Gateway represents all information to configure a gateway
type Gateway struct {
	clientset       *clv1alpha1.SubmarinerV1alpha1Client
	nic             string
	netmask         string
	defaultGW       string
	configNamespace string
	ipConfigName    string
}

// NewGateway returns an Gateway instance
func NewGateway(clientset *clv1alpha1.SubmarinerV1alpha1Client,
	nic string,
	netmask string,
	defaultGW string,
	configNamespace string,
	ipConfigName string) *Gateway {

	return &Gateway{
		clientset:       clientset,
		nic:             nic,
		netmask:         netmask,
		defaultGW:       defaultGW,
		configNamespace: configNamespace,
		ipConfigName:    ipConfigName,
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
	watch, err := g.clientset.Gateways(g.configNamespace).Watch(opts)
	if err != nil {
		panic(err.Error())
	}
	go func() {
		for event := range watch.ResultChan() {
			glog.Errorf("Type: %v", event.Type)
			gw, ok := event.Object.(*v1alpha1.Gateway)
			if !ok {
				glog.Errorf("Not a forwarder: %v", event.Object)
				continue
			}
			if needSync(gw) {
				if err := setSyncing(g.clientset, g.configNamespace, gw); err != nil {
					// TODO: requeue the event
					continue
				}

				if err := g.SyncRule(gw); err != nil {
					// TODO: requeue the event
					continue
				}

				if err := setSynced(g.clientset, g.configNamespace, gw); err != nil {
					// TODO: requeue the event
					continue
				}
			}
		}
	}()

	// Wait forever
	select {}
}

func (g *Gateway) SyncRule(gw *v1alpha1.Gateway) error {
	if err := g.setupIP(gw.Spec.GatewayIP); err != nil {
		return err
	}
	// Apply iptables rules for gw
	if err := g.applyIptablesRules(gw); err != nil {
		return err
	}

	return nil
}

func (g *Gateway) setupIP(ip string) error {
	glog.Infof("Setting up network namespace and ip for %s\n", ip)
	ns, err := util.GetNs(ip)
	if err != nil {
		return err
	}
	vlanDev, err := util.GetVlanDev(ip)
	if err != nil {
		return err
	}

	// Create ns if not exists
	if err := g.ensureNs(ns); err != nil {
		return err
	}

	// Create vlan device if not exists
	if err := g.ensureVlanDev(ns, vlanDev); err != nil {
		return err
	}

	// Link up devices
	cmd := exec.Command("ip", "netns", "exec", ns, "ip", "link", "set", "lo", "up")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("setupIP: failed to link up lo in %q: %v", ns, err)
	}
	cmd = exec.Command("ip", "netns", "exec", ns, "ip", "link", "set", vlanDev, "up")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("setupIP: failed to link up %q in %q: %v", vlanDev, ns, err)
	}

	// Assign IP to vlan device if not assigned
	if err := g.ensureIPforVlanDev(vlanDev, ns, ip); err != nil {
		return err
	}

	// Set default gateway for vlan device if not set
	if err := g.ensureDefaultGW(ns); err != nil {
		return err
	}

	// Make sshd run in ns if not running
	if err := g.ensureSshdRunning(ns, ip); err != nil {
		return err
	}

	return nil
}

func (g *Gateway) ensureNs(ns string) error {
	// Get existing namespaces
	cmd := exec.Command("ip", "netns")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("ensureNs: failed to get existing namespaces: %v", err)
	}

	// Check if namespace ns already exists
	found := false
	for _, s := range strings.Split(string(out), "\n") {
		fields := strings.Fields(s)
		if len(fields) > 0 && fields[0] == ns {
			found = true
			break
		}
	}
	// Skip creating ns if already exists
	if found {
		return nil
	}
	// Create namespace ns
	cmd = exec.Command("ip", "netns", "add", ns)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ensureNs: failed to create namespace %q: %v", ns, err)
	}

	return nil
}

func (g *Gateway) ensureVlanDev(ns, vlanDev string) error {
	//  Getting vlanDev in ns
	cmd := exec.Command("ip", "netns", "exec", ns, "ip", "link", "show", vlanDev)
	err := cmd.Run()
	if err == nil {
		// Already exists
		return nil
	}
	if cmd.ProcessState.ExitCode() != 1 {
		// Other than not found error
		return fmt.Errorf("ensureVlanDev: failed to check if %q exists in %q: %v", vlanDev, ns, err)
	}

	// Create vlanDev
	cmd = exec.Command("ip", "link", "add", vlanDev, "link", g.nic, "type", "macvlan", "mode", "bridge")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ensureVlanDev: failed to create vlan device %q: %v", vlanDev, err)
	}

	// Move vlanDev to ns
	cmd = exec.Command("ip", "link", "set", vlanDev, "netns", ns)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ensureVlanDev: failed to move vlan device %q to %q: %v", vlanDev, ns, err)
	}

	return nil
}

func (g *Gateway) ensureIPforVlanDev(vlanDev, ns, ip string) error {
	// Get current IP
	cmd := exec.Command("ip", "netns", "exec", ns, "ip", "-4", "-o", "addr", "show", vlanDev)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("ensureIPforVlanDev: failed to get ip for %q: %v", vlanDev, err)
	}

	s := strings.Fields(string(out))
	// Already assined
	if len(s) > 3 && s[3] == ip+"/"+g.netmask {
		return nil
	}

	cmd = exec.Command("ip", "netns", "exec", ns, "ip", "addr", "add", ip+"/"+g.netmask, "dev", vlanDev)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ensureIPforVlanDev: failed to assign %q to %q in %q: %v", ip, vlanDev, ns, err)
	}

	return nil
}

func (g *Gateway) ensureDefaultGW(ns string) error {
	cmd := exec.Command("ip", "netns", "exec", ns, "ip", "route")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("ensureDefaultGW: failed to get route for %q: %v", ns, err)
	}

	// Check if default gateway is already set
	found := false
	for _, s := range strings.Split(string(out), "\n") {
		fields := strings.Fields(s)
		if len(fields) > 3 && fields[0] == "default" && fields[1] == "via" && fields[2] == g.defaultGW {
			found = true
			break
		}
	}
	// Skip creating gateway if already set
	if found {
		return nil
	}

	// Set namespace
	cmd = exec.Command("ip", "netns", "exec", ns, "ip", "route", "add", "default", "via", g.defaultGW)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ensureDefaultGW: failed to set gateway %q to %q: %v", g.defaultGW, ns, err)
	}

	return nil
}

func (g *Gateway) ensureSshdRunning(ns, ip string) error {
	pidFile, err := util.GetSshdPidFile(ip)
	if err != nil {
		return fmt.Errorf("ensureSshdRunning: failed to getPidFile for %q: %v", ip, err)
	}

	// Check if pidFile exists to check if sshd running
	if _, err := os.Stat(pidFile); err == nil {
		// sshd already running
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("ensureSshdRunning: failed to check if sshd running in %q: %v", ns, err)
	}

	cmd := exec.Command("ip", "netns", "exec", ns, sshdCommand, "-o", "PidFile="+pidFile)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ensureSshdRunning: failed to exec sshd for %q: %v", ns, err)
	}

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
	glog.Infof("Applying iptables rules for %s\n", gw.Name)
	ns, err := util.GetNs(gw.Spec.GatewayIP)
	if err != nil {
		return err
	}
	// Clear existing iptables rules in ns
	cmd := exec.Command("ip", "netns", "exec", ns, "iptables", "-t", "nat", "-F")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("applyIptablesRules: failed to clear iptables rules in %q: %v", ns, err)
	}

	// Apply all iptables rules
	errStr := ""
	for _, rule := range getExpectedIptablesRule(gw) {
		args := []string{"netns", "exec", ns, "iptables", "-A"}
		ruleStrs := strings.Fields(rule)
		if len(ruleStrs) == 0 {
			// Skip empty rule
			continue
		}
		args = append(args, ruleStrs...)
		cmd := exec.Command("ip", args...)
		if err := cmd.Run(); err != nil {
			// Append error and continue
			errStr = errStr + fmt.Sprintf("failed to apply iptables rule %q: %v, ", rule, err)
		}
	}

	if errStr != "" {
		return fmt.Errorf("applyIptablesRules: in %q: %q", ns, errStr)
	}
	return nil
}
