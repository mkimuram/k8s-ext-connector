package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/golang/glog"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

const (
	// nsPrefix is a prefix for a network namespace used for gateway
	nsPrefix = "ns"
	// vlanDevPrefix is a prefix for a vlan device used for gateway
	vlanDevPrefix = "macvlan"
	// pidFilePath is a path for pid file
	pidFilePath = "/run/"
	// sshdPidFilePrefix is a preffix for pid file of sshd
	sshdPidFilePrefix = "sshd-"
	// pidFileSuffix is a suffix for pid file
	pidFileSuffix = ".pid"
	// sshdCommand is a path to sshd command (TODO: Need to make this variable or auto detect?)
	sshdCommand = "/usr/sbin/sshd"
	// gatewayRulePrefix is a prefix for gateway rule configmap name
	gatewayRulePrefix = "gwrule"
)

var (
	kubeconfig      *string
	clientset       *kubernetes.Clientset
	nic             = flag.String("nic", "eth0", "Name of the nic for parent device of the macvlan device.")
	netmask         = flag.String("netmask", "24", "Netmask for the gateway in numerical format.")
	defaultGW       = flag.String("defaultGW", "192.168.122.1", "Default gateway for the device.")
	configNamespace = flag.String("configNamespace", "external-services", "Kubernetes's namespace that configmap exists.")
	ipConfigName    = flag.String("configName", "ips", "Name of the configmap that contains list of IPs.")

	gw *gateway
)

type gateway struct {
	clientset       *kubernetes.Clientset
	nic             string
	netmask         string
	defaultGW       string
	configNamespace string
	ipConfigName    string
}

func init() {
	//var kubeconfig *string
	if home := os.Getenv("HOME"); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		glog.Errorf("Failed to build config from %q: %v", *kubeconfig, err)
		os.Exit(1)
	}

	// create the clientset
	clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		glog.Errorf("Failed to create client from %q: %v", *kubeconfig, err)
		os.Exit(1)
	}

	gw = newGateway(clientset, *nic, *netmask, *defaultGW, *configNamespace, *ipConfigName)
}

func main() {
	gw.reconcile()
}

func newGateway(clientset *kubernetes.Clientset,
	nic string,
	netmask string,
	defaultGW string,
	configNamespace string,
	ipConfigName string) *gateway {

	return &gateway{
		clientset:       clientset,
		nic:             nic,
		netmask:         netmask,
		defaultGW:       defaultGW,
		configNamespace: configNamespace,
		ipConfigName:    ipConfigName,
	}
}

func (g *gateway) reconcile() {
	// Apply all to initialize
	for {
		err := g.applyAll()
		if err == nil {
			break
		}
		glog.Errorf("reconcile: %v", err)

		time.Sleep(10 * time.Second)
	}
	// TODO: watch configmaps and apply changes if needed
}

func (g *gateway) applyAll() error {
	ips, err := g.getIPs()
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

func (g *gateway) getIPs() ([]string, error) {
	cm, err := clientset.CoreV1().ConfigMaps(g.configNamespace).Get(g.ipConfigName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return []string{}, fmt.Errorf("getIPs: configmap %q in %q namespace not found: %v", g.ipConfigName, g.configNamespace, err)
		}
		return []string{}, err
	}

	if ips, ok := cm.Data["ips"]; ok {
		return strings.Fields(ips), nil
	}

	return []string{}, fmt.Errorf("getIPs: configmap %q in %q namespace not contains ips data", g.ipConfigName, g.configNamespace)
}

func (g *gateway) setupIP(ip string) error {
	ns, err := getNs(ip)
	if err != nil {
		return err
	}
	vlanDev, err := getVlanDev(ip)
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

func (g *gateway) ensureNs(ns string) error {
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

func (g *gateway) ensureVlanDev(ns, vlanDev string) error {
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
	cmd = exec.Command("ip", "link", "add", vlanDev, "link", *nic, "type", "macvlan", "mode", "bridge")
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

func (g *gateway) ensureIPforVlanDev(vlanDev, ns, ip string) error {
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

func (g *gateway) ensureDefaultGW(ns string) error {
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

func (g *gateway) ensureSshdRunning(ns, ip string) error {
	pidFile, err := getPidFile(ip, sshdPidFilePrefix)
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

func (g *gateway) applyIptablesRules(ip string) error {
	ns, err := getNs(ip)
	if err != nil {
		return err
	}

	ruleName, err := getRuleName(ip)
	if err != nil {
		return err
	}
	rules, err := g.getIptablesRules(ruleName)
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

func (g *gateway) getIptablesRules(name string) ([]string, error) {
	cm, err := clientset.CoreV1().ConfigMaps(g.configNamespace).Get(name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return []string{}, fmt.Errorf("getIptablesRules: configmap %q in %q namespace not found: %v", g.configNamespace, name, err)
		}
		return []string{}, err
	}

	if rules, ok := cm.Data["rules"]; ok {
		return strings.Split(rules, "\n"), nil
	}

	return []string{}, fmt.Errorf("getIptablesRules: configmap %q in %q namespace not contains rules data", g.configNamespace, name)
}

func getHexIP(ip string) (string, error) {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return "", fmt.Errorf("getHexIP: failed to parse ip %q", ip)
	}
	v4IP := parsedIP.To4()
	if v4IP == nil {
		return "", fmt.Errorf("getHexIP: failed to convert ip %v to 4 bytes", parsedIP)
	}

	return fmt.Sprintf("%02x%02x%02x%02x", v4IP[0], v4IP[1], v4IP[2], v4IP[3]), nil
}

func getNs(ip string) (string, error) {
	hexIP, err := getHexIP(ip)
	if err != nil {
		return "", err
	}

	return nsPrefix + hexIP, nil
}

func getVlanDev(ip string) (string, error) {
	hexIP, err := getHexIP(ip)
	if err != nil {
		return "", err
	}

	return vlanDevPrefix + hexIP, nil
}

func getPidFile(ip, prefix string) (string, error) {
	hexIP, err := getHexIP(ip)
	if err != nil {
		return "", err
	}
	pidFileName := prefix + hexIP + pidFileSuffix

	return filepath.Join(pidFilePath, pidFileName), nil
}

func getRuleName(ip string) (string, error) {
	hexIP, err := getHexIP(ip)
	if err != nil {
		return "", err
	}

	return gatewayRulePrefix + hexIP, nil
}
