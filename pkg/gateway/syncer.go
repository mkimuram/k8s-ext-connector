package gateway

import (
	"context"

	backoffv4 "github.com/cenkalti/backoff/v4"
	"github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	"github.com/mkimuram/k8s-ext-connector/pkg/util"
)

const (
	prechainPrefix  = "pre"
	postchainPrefix = "pst"
)

type GatewaySyncer struct {
	ssh map[string]context.CancelFunc
}

type GatewaySyncerInterface interface {
	syncRule(gw *v1alpha1.Gateway) error
	ruleSynced(gw *v1alpha1.Gateway) bool
}

var _ GatewaySyncerInterface = &GatewaySyncer{}

func NewGatewaySyncer() *GatewaySyncer {
	return &GatewaySyncer{
		ssh: map[string]context.CancelFunc{},
	}
}

func (g *GatewaySyncer) syncRule(gw *v1alpha1.Gateway) error {
	if err := g.ensureSshdRunning(gw.Spec.GatewayIP); err != nil {
		return err
	}
	// Apply iptables rules for gw
	if err := g.applyIptablesRules(gw); err != nil {
		return err
	}

	return nil
}

func (g *GatewaySyncer) ensureSshdRunning(ip string) error {
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

func (g *GatewaySyncer) applyIptablesRules(gw *v1alpha1.Gateway) error {
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

func (g *GatewaySyncer) ruleSynced(gw *v1alpha1.Gateway) bool {
	return g.checkSshdRunning(gw.Spec.GatewayIP) && g.checkIptablesRulesApplied(gw)
}

func (g *GatewaySyncer) checkSshdRunning(ip string) bool {
	// TODO: consider more strict check?
	// below only check that the port is open in the specified ip
	return util.IsPortOpen(ip, util.SSHPort)
}

func (g *GatewaySyncer) checkIptablesRulesApplied(gw *v1alpha1.Gateway) bool {
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
