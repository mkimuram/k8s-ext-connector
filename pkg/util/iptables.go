package util

import "github.com/coreos/go-iptables/iptables"

const (
	// TableNAT represents nat table in iptables
	TableNAT = "nat"
	// ChainPrerouting represents PREROUTING chain in iptables
	ChainPrerouting = "PREROUTING"
	// ChainPostrouting represents POSTROUTING chain in iptables
	ChainPostrouting = "POSTROUTING"
)

// DNATRuleSpec returns ruleSpec to DNAT for the given arguments
func DNATRuleSpec(dstIP, srcIP, dPort, destinationIP, destinationPort string) []string {
	return []string{"-m", "tcp", "-p", "tcp", "--dst", dstIP, "--src", srcIP, "--dport", dPort, "-j", "DNAT", "--to-destination", destinationIP + ":" + destinationPort}
}

// SNATRuleSpec returns ruleSpec to SNAT for the given arguments
func SNATRuleSpec(dstIP, srcIP, dPort string) []string {
	return []string{"-m", "tcp", "-p", "tcp", "--dst", dstIP, "--dport", dPort, "-j", "SNAT", "--to-source", srcIP}
}

// Defining used interfaces in iptables-go to use mock in unit test
type iptInterface interface {
	ClearChain(table, chain string) error
	AppendUnique(table, chain string, rule ...string) error
	Exists(table, chain string, rule ...string) (bool, error)
}

// ReplaceChains replaces rules in {table} to {expected}.
// Existing rules in the chains will be deleted.
// It returns error if there are any error on replacing chains.
// {expected} is passed as a map of chain name to slice of ruleSpec.
// ex) to specify "-j pre1" and "-j pre2" in "PREROUTING" chain
//   map[string][][]string{"PREROUTING": [][]string{{"-j", "pre1"}, {"-j", "pre2"}}}
func ReplaceChains(table string, expected map[string][][]string) error {
	ipt, err := iptables.New()
	if err != nil {
		return err
	}

	return appendChains(ipt, table, expected, true)
}

// AddChains adds {expected} rules in {table}.
// Existing ruleSpec in the chains won't be deleted.
// It returns error if there are any error on adding chains.
// {expected} is passed as a map of chain name to slice of ruleSpec.
// ex) to specify "-j pre1" and "-j pre2" in "PREROUTING" chain
//   map[string][][]string{"PREROUTING": [][]string{{"-j", "pre1"}, {"-j", "pre2"}}}
func AddChains(table string, expected map[string][][]string) error {
	ipt, err := iptables.New()
	if err != nil {
		return err
	}

	return appendChains(ipt, table, expected, false)
}

func appendChains(ipt iptInterface, table string, expected map[string][][]string, flash bool) error {
	// Flash chain if required
	if flash {
		for chain := range expected {
			// Flash existing rules
			if err := ipt.ClearChain(table, chain); err != nil {
				return err
			}
		}
	}
	// Apply all rules
	for chain, rules := range expected {
		for _, rule := range rules {
			if err := ipt.AppendUnique(table, chain, rule...); err != nil {
				return err
			}
		}
	}

	return nil
}

// CheckChainsExist checks if all {expected} rules exist in {table}.
// It returns error if it fails to find any expected rules or there's error in checking
// {expected} is passed as a map of chain name to slice of ruleSpec.
// ex) to specify "-j pre1" and "-j pre2" in "PREROUTING" chain
//   map[string][][]string{"PREROUTING": [][]string{{"-j", "pre1"}, {"-j", "pre2"}}}
func CheckChainsExist(table string, expected map[string][][]string) bool {
	ipt, err := iptables.New()
	if err != nil {
		return false
	}

	return checkChainsExist(ipt, table, expected)
}

func checkChainsExist(ipt iptInterface, table string, expected map[string][][]string) bool {
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
