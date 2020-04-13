package util

import (
	"encoding/hex"
	"fmt"
	"net"
	"path/filepath"
	"strings"

	submarinerv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	"github.com/operator-framework/operator-sdk/pkg/status"
	corev1 "k8s.io/api/core/v1"
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
	// gatewayRulePrefix is a prefix for gateway rule configmap name
	gatewayRulePrefix = "gwrule"
	// minPort is the smallest port number that can be used by forwarder pod
	MinPort = 2049
	// maxPort is the biggest port number that can be used by forwarder pod
	MaxPort = 65536
)

// RuleUpdatingCondition sets submarinerv1alpha1.ConditionRuleUpdating to stat
func RuleUpdatingCondition(stat corev1.ConditionStatus) status.Condition {
	return status.Condition{
		Type:   submarinerv1alpha1.ConditionRuleUpdating,
		Status: stat,
	}
}

// RuleSyncingCondition sets submarinerv1alpha1.ConditionRuleSyncing to stat
func RuleSyncingCondition(stat corev1.ConditionStatus) status.Condition {
	return status.Condition{
		Type:   submarinerv1alpha1.ConditionRuleSyncing,
		Status: stat,
	}
}

// GetHexIP returns hex expression of IP address
// ex) 192.168.122.1 -> c0a87a01
func GetHexIP(ip string) (string, error) {
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

// GetDecIPFromHexIP returns decimal expression of IP address
// ex) c0a87a01 -> 192.168.122.1
func GetDecIPFromHexIP(hexIP string) (string, error) {
	a, err := hex.DecodeString(hexIP)
	if err != nil {
		return "", err
	}
	if len(a) != 4 {
		return "", fmt.Errorf("%s isn't a valid IP", hexIP)
	}

	return fmt.Sprintf("%v.%v.%v.%v", a[0], a[1], a[2], a[3]), nil
}

// GetRuleName returns configmap name for gateway which has ip
// ex) 192.168.122.1 -> gwrulec0a87a01
func GetRuleName(ip string) (string, error) {
	hexIP, err := GetHexIP(ip)
	if err != nil {
		return "", err
	}

	return gatewayRulePrefix + hexIP, nil
}

// GetIPfromRuleName returns IP for configmap
// ex) gwrulec0a87a01 -> 192.168.122.1
func GetIPfromRuleName(ruleName string) (string, error) {
	return GetDecIPFromHexIP(strings.TrimPrefix(ruleName, gatewayRulePrefix))
}

// GetNs returns network namespace name for gateway which has ip
// ex) 192.168.122.1 -> nsc0a87a01
func GetNs(ip string) (string, error) {
	hexIP, err := GetHexIP(ip)
	if err != nil {
		return "", err
	}

	return nsPrefix + hexIP, nil
}

// GetVlanDev returns vlandevice name for gateway which has ip
// ex) 192.168.122.1 -> macvlanc0a87a01
func GetVlanDev(ip string) (string, error) {
	hexIP, err := GetHexIP(ip)
	if err != nil {
		return "", err
	}

	return vlanDevPrefix + hexIP, nil
}

// GetSshdPidFile returns name of pid file for sshd running in namespace which has ip
// ex) 192.168.122.1 -> sshd-c0a87ac8.pid
func GetSshdPidFile(ip string) (string, error) {
	return GetPidFile(ip, sshdPidFilePrefix)
}

// GetPidFile returns name of pid file in namespace which has ip
// ex) 192.168.122.1 -> {prefix}c0a87ac8.pid
func GetPidFile(ip, prefix string) (string, error) {
	hexIP, err := GetHexIP(ip)
	if err != nil {
		return "", err
	}
	pidFileName := prefix + hexIP + pidFileSuffix

	return filepath.Join(pidFilePath, pidFileName), nil
}
