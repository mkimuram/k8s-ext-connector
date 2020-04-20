package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/golang/glog"
	v1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	clv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/client/clientset/versioned/typed/submariner/v1alpha1"
	"github.com/mkimuram/k8s-ext-connector/pkg/util"
	"golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
)

var (
	ns   string
	name string
)

func init() {
	flag.Set("logtostderr", "true")
	flag.Set("stderrthreshold", "INFO")
	flag.Parse()
	ns = os.Getenv("FORWARDER_NAMESPACE")
	name = os.Getenv("FORWARDER_NAME")

	if ns == "" || name == "" {
		glog.Fatalf("FORWARDER_NAMESPACE and FORWARDER_NAME need to be defined as environment variables")
	}
}

type Forwarder struct {
	clientset     *clv1alpha1.SubmarinerV1alpha1Client
	namespace     string
	tunnels       map[string]*util.Tunnel
	remoteTunnels map[string]*util.Tunnel
	config        *ssh.ClientConfig
}

func NewForwarder(cl *clv1alpha1.SubmarinerV1alpha1Client, ns string) *Forwarder {
	// TODO: Create clientconfig properly
	user := "root"
	password := "password"
	return &Forwarder{
		clientset:     cl,
		namespace:     ns,
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

func (f *Forwarder) toTunnel(tun string) *util.Tunnel {
	s := strings.Split(tun, ":")
	local := fmt.Sprintf("%s:%s", s[0], s[1])
	server := fmt.Sprintf("%s:%s", s[2], s[3])
	remote := fmt.Sprintf("%s:%s", s[4], s[5])

	return util.NewTunnel(local, server, remote, f.config)
}

func (f *Forwarder) deleteUnusedSSHTunnel(expected map[string]bool) {
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

func (f *Forwarder) ensureSSHTunnel(expected map[string]bool) {
	created := map[string]*util.Tunnel{}
	for k, _ := range expected {
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

func (f *Forwarder) deleteUnusedRemoteSSHTunnel(expected map[string]bool) {
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

func (f *Forwarder) ensureRemoteSSHTunnel(expected map[string]bool) {
	created := map[string]*util.Tunnel{}
	for k, _ := range expected {
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

func (f *Forwarder) updateSSHTunnel(expected map[string]bool) {
	f.deleteUnusedSSHTunnel(expected)
	f.ensureSSHTunnel(expected)
}

func (f *Forwarder) updateRemoteSSHTunnel(expected map[string]bool) {
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

func needSync(fwd *v1alpha1.Forwarder) bool {
	// Sync is needed if
	// - generations are different between rule and sync &&
	// - rule is not updating
	return fwd.Status.RuleGeneration != fwd.Status.SyncGeneration &&
		fwd.Status.Conditions.IsFalseFor(v1alpha1.ConditionRuleUpdating)
}

func setSyncing(clientset *clv1alpha1.SubmarinerV1alpha1Client, ns string, fwd *v1alpha1.Forwarder) error {
	var err error
	if fwd.Status.Conditions.SetCondition(util.RuleSyncingCondition(corev1.ConditionTrue)) {
		fwd, err = clientset.Forwarders(ns).UpdateStatus(fwd)
		if err != nil {
			return err
		}
		glog.Infof("Update RuleSyncingCondition to true")
	}
	return nil
}

func setSynced(clientset *clv1alpha1.SubmarinerV1alpha1Client, ns string, fwd *v1alpha1.Forwarder) error {
	var err error
	if fwd.Status.Conditions.SetCondition(util.RuleSyncingCondition(corev1.ConditionFalse)) {
		fwd.Status.SyncGeneration = fwd.Status.RuleGeneration
		fwd, err = clientset.Forwarders(ns).UpdateStatus(fwd)
		if err != nil {
			return err
		}
		glog.Infof("Update RuleSyncingCondition to false")
	}
	return nil
}

func (f *Forwarder) SyncRule(fwd *v1alpha1.Forwarder) error {
	st := getExpectedSSHTunnel(fwd)
	glog.Infof("ExpectedSSHTunnel: %v", st)
	f.updateSSHTunnel(st)

	rt := getExpectedRemoteSSHTunnel(fwd)
	glog.Infof("ExpectedRemoteSSHTunnel: %v", rt)
	f.updateRemoteSSHTunnel(rt)

	it := getExpectedIptablesRule(fwd)
	glog.Infof("ExpectedIptablesRule: %v", it)
	if err := updateIptablesRule(it); err != nil {
		glog.Errorf("failed to update iptables rule: %v", err)
		return err
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

	opts := metav1.ListOptions{FieldSelector: fmt.Sprintf("metadata.name=%s", name)}

	watch, err := clientset.Forwarders(ns).Watch(opts)
	if err != nil {
		panic(err.Error())
	}
	go func() {
		f := NewForwarder(clientset, ns)
		for event := range watch.ResultChan() {
			glog.Errorf("Type: %v", event.Type)
			fwd, ok := event.Object.(*v1alpha1.Forwarder)
			if !ok {
				glog.Errorf("Not a forwarder: %v", event.Object)
				continue
			}
			if needSync(fwd) {
				if err := setSyncing(clientset, ns, fwd); err != nil {
					// TODO: requeue the event
					continue
				}
				if err := f.SyncRule(fwd); err != nil {
					// TODO: requeue the event
					continue
				}
				if err := setSynced(clientset, ns, fwd); err != nil {
					// TODO: requeue the event
					continue
				}
			}
		}
	}()

	// Wait forever
	select {}
}
