package main

import (
	"flag"
	"fmt"
	"os/exec"
	"strings"

	"github.com/golang/glog"
	v1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	clv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/client/clientset/versioned/typed/submariner/v1alpha1"
	"github.com/mkimuram/k8s-ext-connector/pkg/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
)

func init() {
	flag.Set("logtostderr", "true")
	flag.Set("stderrthreshold", "INFO")
	flag.Parse()
}

func getExistingTunnel(options string) (map[string]string, error) {
	ret := map[string]string{}
	// Get sshd process
	// Expected output format is:
	//   {pid} {args}...
	// ex)
	//   2149231 ssh -o StrictHostKeyChecking=no -i /etc/ssh-key/id_rsa -f -N -R 192.168.122.201:2049:10.96.218.78:80 192.168.122.201
	//   2747420 ssh -o StrictHostKeyChecking=no -i /etc/ssh-key/id_rsa -g -f -N -L 2049:192.168.122.140:8000 192.168.122.201
	cmd := exec.Command("ps", "-C", "ssh", "-o", "pid,args", "--no-headers")
	out, err := cmd.Output()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			// ps command will return exit code 1 if no process found,
			// so return error only when it returns other than 1
			if exitError.ExitCode() != 1 {
				return ret, fmt.Errorf("failed to get ssh process: %v", err)
			}
		}
	}

	// Get only pids that has {options} in arguments, and put them to map
	// {options} will be "-g -f -N -L" or "-f -N -R"
	for _, s := range strings.Split(string(out), "\n") {
		if !strings.Contains(s, options) {
			// Skip unmatched line
			continue
		}
		fields := strings.Fields(s)
		// fields needs to be longer than 2, to access to fields[len(fields)-2] below
		if len(fields) < 2 {
			glog.Warningf("invalid process string %q: %v", s, err)
			continue
		}
		// pid should be "2747420" in above case, if {options} is "-g -f -N -L"
		pid := fields[0]
		// args should be "2049:192.168.122.140:8000 192.168.122.201" in above case
		// if {options} is "-g -f -N -L"
		args := fmt.Sprintf("%s %s", fields[len(fields)-2], fields[len(fields)-1])
		ret[args] = pid
	}

	return ret, nil
}

func deleteUnusedTunnel(expected []string, options string) error {
	expectedMap := map[string]bool{}
	for _, val := range expected {
		expectedMap[val] = true
	}

	existing, err := getExistingTunnel(options)
	if err != nil {
		return err
	}

	for tun, pid := range existing {
		if _, ok := expectedMap[tun]; ok {
			// Existing tunnel is expected, no need to delete this tunnel
			continue
		}
		// delete unused tunnel
		cmd := exec.Command("kill", pid)
		if err := cmd.Run(); err != nil {
			// TODO: handle error properly
			glog.Errorf("failed to execute command %v: %v", cmd, err)
		}
	}

	return nil
}

func ensureTunnel(expected []string, options string) error {
	existing, err := getExistingTunnel(options)
	if err != nil {
		return err
	}

	for _, tun := range expected {
		if _, ok := existing[tun]; ok {
			// Already exists, so skip creating tunnel
			continue
		}

		args := []string{"-o", "StrictHostKeyChecking=no", "-i", "/etc/ssh-key/id_rsa"}
		optionStrs := strings.Fields(options)
		args = append(args, optionStrs...)
		tunStrs := strings.Fields(tun)
		if len(tunStrs) == 0 {
			// Skip empty rule
			continue
		}
		args = append(args, tunStrs...)
		cmd := exec.Command("ssh", args...)
		if err := cmd.Start(); err != nil {
			// TODO: handle error properly
			glog.Errorf("failed to execute command %v: %v", cmd, err)
		}
	}

	return nil
}

func updateTunnel(expected []string, options string) error {
	if err := deleteUnusedTunnel(expected, options); err != nil {
		return err
	}

	return ensureTunnel(expected, options)
}

func updateSSHTunnel(expected []string) error {
	return updateTunnel(expected, "-g -f -N -L" /* options */)
}

func updateRemoteSSHTunnel(expected []string) error {
	return updateTunnel(expected, "-f -N -R" /* options */)
}

func updateIptablesRule(expected []string) error {
	// Flash existing nat rules
	cmd := exec.Command("iptables", "-t", "nat", "-F")
	if err := cmd.Run(); err != nil {
		glog.Errorf("failed to execute command %v: %v", cmd, err)
		return err
	}

	// Apply all rules
	for _, rule := range expected {
		args := []string{"-A"}
		ruleStrs := strings.Fields(rule)
		if len(ruleStrs) == 0 {
			// Skip empty rule
			continue
		}
		args = append(args, ruleStrs...)
		cmd := exec.Command("iptables", args...)

		if err := cmd.Run(); err != nil {
			// TODO: handle error properly
			glog.Errorf("failed to execute command %v: %v", cmd, err)
		}
	}

	return nil
}

func getExpectedSSHTunnel(fwd *v1alpha1.Forwarder) []string {
	st := []string{}
	// Format fwd.Spec.EgressRules to
	//   {RelayPort}:{DestinationIp}:{DestinationPort} {GatewayIP}
	// ex)
	//   "2049:192.168.122.140:8000 192.168.122.201"
	for _, rule := range fwd.Spec.EgressRules {
		st = append(st, fmt.Sprintf("%s:%s:%s %s", rule.RelayPort, rule.DestinationIP, rule.DestinationPort, rule.GatewayIP))
	}

	return st
}

func getExpectedRemoteSSHTunnel(fwd *v1alpha1.Forwarder) []string {
	rt := []string{}
	// Format fwd.Spec.IngressRules to
	//   {GatewayIP}:{RelayPort}:{DestinationIp}:{DestinationPort} {GatewayIP}
	// ex)
	//   "192.168.122.201:2049:10.96.218.78:80 192.168.122.201"

	// TODO

	for _, rule := range fwd.Spec.IngressRules {
		rt = append(rt, fmt.Sprintf("%s:%s:%s:%s %s", rule.GatewayIP, rule.RelayPort, rule.DestinationIP, rule.DestinationPort, rule.GatewayIP))
	}

	return rt
}

func getExpectedIptablesRule(fwd *v1alpha1.Forwarder) []string {
	it := []string{}
	// Format fwd.Spec.EgressRules to
	//   PREROUTING -t nat -m tcp -p tcp --dst {ForwarderIP} --src {SourceIP} --dport {TargetPort} -j DNAT --to-destination {ForwarderIp}:{RelayPort}
	//   POSTROUTING -t nat -m tcp -p tcp --dst {DestinationIP} --dport {RelayPort} -j SNAT --to-source {ForwarderIP}
	// ex)
	//   "PREROUTING -t nat -m tcp -p tcp --dst 10.244.0.34 --src 10.244.0.11 --dport 8000 -j DNAT --to-destination 10.244.0.34:2049"
	//   "POSTROUTING -t nat -m tcp -p tcp --dst 192.168.122.139 --dport 2049 -j SNAT --to-source 10.244.0.34"
	// TODO: Also handle UDP properly
	for _, rule := range fwd.Spec.EgressRules {
		it = append(it, fmt.Sprintf("PREROUTING -t nat -m tcp -p tcp --dst %s --src %s --dport %s -j DNAT --to-destination %s:%s\n", fwd.Spec.ForwarderIP, rule.SourceIP, rule.TargetPort, fwd.Spec.ForwarderIP, rule.RelayPort))
		it = append(it, fmt.Sprintf("POSTROUTING -t nat -m tcp -p tcp --dst %s --dport %s -j SNAT --to-source %s\n", rule.DestinationIP, rule.RelayPort, fwd.Spec.ForwarderIP))
	}

	return it
}

func action(cl *clv1alpha1.SubmarinerV1alpha1Client, ns string, fwd *v1alpha1.Forwarder) error {
	var err error
	if fwd.Status.Conditions.SetCondition(util.RuleSyncingCondition(corev1.ConditionTrue)) {
		fwd, err = cl.Forwarders(ns).UpdateStatus(fwd)
		if err != nil {
			return err
		}
		glog.Infof("Update RuleSyncingCondition to true")
	}

	st := getExpectedSSHTunnel(fwd)
	glog.Infof("ExpectedSSHTunnel: %v", st)
	if err := updateSSHTunnel(st); err != nil {
		glog.Errorf("failed to update ssh tunnel: %v", err)
		return err
	}

	rt := getExpectedRemoteSSHTunnel(fwd)
	glog.Infof("ExpectedRemoteSSHTunnel: %v", rt)
	if err := updateRemoteSSHTunnel(rt); err != nil {
		glog.Errorf("failed to update remote ssh tunnel: %v", err)
		return err
	}

	it := getExpectedIptablesRule(fwd)
	glog.Infof("ExpectedIptablesRule: %v", it)
	if err := updateIptablesRule(it); err != nil {
		glog.Errorf("failed to update iptables rule: %v", err)
		return err
	}

	if fwd.Status.Conditions.SetCondition(util.RuleSyncingCondition(corev1.ConditionFalse)) {
		fwd.Status.SyncGeneration = fwd.Status.RuleGeneration
		fwd, err = cl.Forwarders(ns).UpdateStatus(fwd)
		if err != nil {
			return err
		}
		glog.Infof("Update RuleSyncingCondition to false")
	}

	return nil
}

func main() {
	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	// creates the clientset
	clientset, err := clv1alpha1.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	ns := "external-services"
	name := "my-externalservice"
	opts := metav1.ListOptions{FieldSelector: fmt.Sprintf("metadata.name=%s", name)}

	watch, err := clientset.Forwarders(ns).Watch(opts)
	if err != nil {
		panic(err.Error())
	}
	go func() {
		for event := range watch.ResultChan() {
			glog.Errorf("Type: %v", event.Type)
			fwd, ok := event.Object.(*v1alpha1.Forwarder)
			if !ok {
				glog.Errorf("Not a forwarder: %v", event.Object)
				continue
			}
			// Generations are different between rule and sync &&
			// rule is not syncing  && updating == false means, we need to take action
			if fwd.Status.RuleGeneration != fwd.Status.SyncGeneration &&
				!fwd.Status.Conditions.IsFalseFor(v1alpha1.ConditionRuleSyncing) &&
				fwd.Status.Conditions.IsFalseFor(v1alpha1.ConditionRuleUpdating) {
				action(clientset, ns, fwd)
			}
		}
	}()

	// TODO: Add codes to occasionally check the current status really synced,
	//       because network errors or gateway changes might make it out of sync.

	// Wait forever
	select {}
}
