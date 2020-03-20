package gateway

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/mkimuram/k8s-ext-connector/pkg/util"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

const (
	// sshdCommand is a path to sshd command (TODO: Need to make this variable or auto detect?)
	sshdCommand = "/usr/sbin/sshd"
)

// Gateway represents all information to configure a gateway
type Gateway struct {
	clientset       *kubernetes.Clientset
	nic             string
	netmask         string
	defaultGW       string
	configNamespace string
	ipConfigName    string
}

// NewGateway returns an Gateway instance
func NewGateway(clientset *kubernetes.Clientset,
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

// Reconcile reconciles the gateway configuration to the desired state
func (g *Gateway) Reconcile() {
	// Apply all to initialize
	for {
		err := g.applyAll()
		if err == nil {
			break
		}
		glog.Errorf("reconcile: %v", err)

		time.Sleep(10 * time.Second)
	}

	// Watch configmaps and apply changes if needed
	watchlist := cache.NewListWatchFromClient(
		g.clientset.CoreV1().RESTClient(),
		string(v1.ResourceConfigMaps),
		g.configNamespace,
		fields.Everything(),
	)

	_, controller := cache.NewInformer(
		watchlist,
		&v1.ConfigMap{},
		0,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				g.handleChanges(obj)
			},
			DeleteFunc: func(obj interface{}) {
				// TODO: handle deletion properly
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				g.handleChanges(newObj)
			},
		},
	)
	stop := make(chan struct{})
	defer close(stop)
	go controller.Run(stop)
	for {
		time.Sleep(time.Second)
	}
}

func (g *Gateway) handleChanges(obj interface{}) {
	configMap, ok := obj.(*v1.ConfigMap)
	if !ok {
		// Not a configmap
		glog.Infof("Not a configmap %v", obj)
		return
	}

	// Handle configuration change for IPs
	if configMap.Name == g.ipConfigName {
		glog.Infof("Call applyAll to %q", configMap.Name)
		g.applyAll()
		// TODO: Handle error prorperly
		return
	}

	// Handle configuration change for gateway
	if util.IsGatewayRule(configMap.Name) {
		ip, err := util.GetIPfromRuleName(configMap.Name)
		if err == nil {
			glog.Infof("Call applyIptablesRules to %q %q", configMap.Name, ip)
			// TODO: Handle error prorperly
			g.applyIptablesRules(ip)
		}
	}
}

func (g *Gateway) applyAll() error {
	ips, err := util.GetIPs(g.clientset, g.configNamespace, g.ipConfigName)
	if err != nil {
		return err
	}

	errStr := ""
	for _, ip := range ips {
		// Set up gateway for IP
		glog.Infof("Setting up gateway for %q\n", ip)
		if err := g.setupIP(ip); err != nil {
			errStr = errStr + err.Error() + ", "
			// Skip applying iptables rule below
			continue
		}
		// Apply iptables rules for IP
		if err := g.applyIptablesRules(ip); err != nil {
			errStr = errStr + err.Error() + ", "
		}
	}

	// Return error if there are any errors
	if errStr != "" {
		return fmt.Errorf("applyAll: %s", errStr)
	}

	return nil
}

func (g *Gateway) setupIP(ip string) error {
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

func (g *Gateway) applyIptablesRules(ip string) error {
	glog.Infof("Applying iptables rules for %q\n", ip)
	ns, err := util.GetNs(ip)
	if err != nil {
		return err
	}

	ruleName, err := util.GetRuleName(ip)
	if err != nil {
		return err
	}

	rules, err := util.GetIptablesRules(g.clientset, g.configNamespace, ruleName)
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
	for _, rule := range rules {
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
