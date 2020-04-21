package util

const (
	TableNAT         = "nat"
	ChainPrerouting  = "PREROUTING"
	ChainPostrouting = "POSTROUTING"
)

func DNATRuleSpec(dstIP, dstPort, srcIP, dPort string) []string {
	return []string{"-m", "tcp", "-p", "tcp", "--dst", dstIP, "--src", srcIP, "--dport", dPort, "-j", "DNAT", "--to-destination", dstIP + ":" + dstPort}
}

func SNATRuleSpec(dstIP, srcIP, dPort string) []string {
	return []string{"-m", "tcp", "-p", "tcp", "--dst", dstIP, "--dport", dPort, "-j", "SNAT", "--to-source", srcIP}
}
