package util

import (
	"fmt"
	"net"

	submarinerv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	"github.com/operator-framework/operator-sdk/pkg/status"
	corev1 "k8s.io/api/core/v1"
)

const (
	// gatewayRulePrefix is a prefix for gateway rule configmap name
	gatewayRulePrefix = "gwrule"
	// MinPort is the smallest port number that can be used by forwarder pod
	MinPort = 2049
	// MaxPort is the biggest port number that can be used by forwarder pod
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

// GetRuleName returns configmap name for gateway which has ip
// ex) 192.168.122.1 -> gwrulec0a87a01
func GetRuleName(ip string) (string, error) {
	hexIP, err := GetHexIP(ip)
	if err != nil {
		return "", err
	}

	return gatewayRulePrefix + hexIP, nil
}
