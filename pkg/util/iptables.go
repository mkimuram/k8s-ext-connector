package util

import "github.com/coreos/go-iptables/iptables"

const (
	TableNAT         = "nat"
	ChainPrerouting  = "PREROUTING"
	ChainPostrouting = "POSTROUTING"
)

func DNATRuleSpec(dstIP, srcIP, dPort, destinationIP, destinationPort string) []string {
	return []string{"-m", "tcp", "-p", "tcp", "--dst", dstIP, "--src", srcIP, "--dport", dPort, "-j", "DNAT", "--to-destination", destinationIP + ":" + destinationPort}
}

func SNATRuleSpec(dstIP, srcIP, dPort string) []string {
	return []string{"-m", "tcp", "-p", "tcp", "--dst", dstIP, "--dport", dPort, "-j", "SNAT", "--to-source", srcIP}
}

func ReplaceChains(table string, expected map[string][][]string) error {
	return appendChains(table, expected, true)
}

func AddChains(table string, expected map[string][][]string) error {
	return appendChains(table, expected, false)
}

func appendChains(table string, expected map[string][][]string, flash bool) error {
	ipt, err := iptables.New()
	if err != nil {
		return err
	}

	// Apply all rules
	for chain, rules := range expected {
		if flash {
			// Flash existing rules
			if err := ipt.ClearChain(table, chain); err != nil {
				return err
			}
		}
		for _, rule := range rules {
			if err := ipt.AppendUnique(table, chain, rule...); err != nil {
				return err
			}
		}
	}

	return nil
}

func CheckChainsExist(table string, expected map[string][][]string) bool {
	ipt, err := iptables.New()
	if err != nil {
		return false
	}

	for chain, rules := range expected {
		for _, rule := range rules {
			exists, err := ipt.Exists(table, chain, rule...)
			if err != nil {
				return false
			}
			if !exists {
				return false
			}
		}
	}

	return true
}
