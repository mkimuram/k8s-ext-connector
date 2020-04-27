package util

import (
	"reflect"
	"testing"

	submarinerv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	"github.com/operator-framework/operator-sdk/pkg/status"
	corev1 "k8s.io/api/core/v1"
)

func TestRuleUpdatingCondition(t *testing.T) {
	testCases := []struct {
		name     string
		stat     corev1.ConditionStatus
		expected status.Condition
	}{
		{
			name: "Normal case (set true)",
			stat: corev1.ConditionTrue,
			expected: status.Condition{
				Type:   submarinerv1alpha1.ConditionRuleUpdating,
				Status: corev1.ConditionTrue,
			},
		},
		{
			name: "Normal case (set false)",
			stat: corev1.ConditionFalse,
			expected: status.Condition{
				Type:   submarinerv1alpha1.ConditionRuleUpdating,
				Status: corev1.ConditionFalse,
			},
		},
		{
			name: "Normal case (set unknown)",
			stat: corev1.ConditionUnknown,
			expected: status.Condition{
				Type:   submarinerv1alpha1.ConditionRuleUpdating,
				Status: corev1.ConditionUnknown,
			},
		},
	}

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)

		st := RuleUpdatingCondition(tc.stat)
		if !reflect.DeepEqual(tc.expected, st) {
			t.Errorf("expected %v, but got %v", tc.expected, st)
		}
	}
}

func TestRuleSyncingCondition(t *testing.T) {
	testCases := []struct {
		name     string
		stat     corev1.ConditionStatus
		expected status.Condition
	}{
		{
			name: "Normal case (set true)",
			stat: corev1.ConditionTrue,
			expected: status.Condition{
				Type:   submarinerv1alpha1.ConditionRuleSyncing,
				Status: corev1.ConditionTrue,
			},
		},
		{
			name: "Normal case (set false)",
			stat: corev1.ConditionFalse,
			expected: status.Condition{
				Type:   submarinerv1alpha1.ConditionRuleSyncing,
				Status: corev1.ConditionFalse,
			},
		},
		{
			name: "Normal case (set unknown)",
			stat: corev1.ConditionUnknown,
			expected: status.Condition{
				Type:   submarinerv1alpha1.ConditionRuleSyncing,
				Status: corev1.ConditionUnknown,
			},
		},
	}

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)

		st := RuleSyncingCondition(tc.stat)
		if !reflect.DeepEqual(tc.expected, st) {
			t.Errorf("expected %v, but got %v", tc.expected, st)
		}
	}
}

func TestGetHexIP(t *testing.T) {
	testCases := []struct {
		name      string
		ip        string
		expected  string
		expectErr bool
	}{
		{
			name:      "Normal case (ip=192.168.122.1)",
			ip:        "192.168.122.1",
			expected:  "c0a87a01",
			expectErr: false,
		},
		{
			name:      "Error case (not ip, ip=192.168.122.1.1)",
			ip:        "192.168.122.1.1",
			expected:  "",
			expectErr: true,
		},
		{
			name:      "Error case (ipv6 ip=2001:db8::68)",
			ip:        "2001:db8::68",
			expected:  "",
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)

		hexIP, err := GetHexIP(tc.ip)
		if tc.expectErr {
			if err == nil {
				t.Errorf("expected error, but not got error")
			}
		} else {
			if err != nil {
				t.Errorf("expected no error, but got error %v", err)
			}
			if tc.expected != hexIP {
				t.Errorf("expected %v, but got %v", tc.expected, hexIP)
			}
		}
	}
}
func TestGetRuleName(t *testing.T) {
	testCases := []struct {
		name      string
		ip        string
		expected  string
		expectErr bool
	}{
		{
			name:      "Normal case (ip=192.168.122.1)",
			ip:        "192.168.122.1",
			expected:  "gwrulec0a87a01",
			expectErr: false,
		},
		{
			name:      "Error case (not ip, ip=192.168.122.1.1)",
			ip:        "192.168.122.1.1",
			expected:  "",
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)

		ruleName, err := GetRuleName(tc.ip)
		if tc.expectErr {
			if err == nil {
				t.Errorf("expected error, but not got error")
			}
		} else {
			if err != nil {
				t.Errorf("expected no error, but got error %v", err)
			}
			if tc.expected != ruleName {
				t.Errorf("expected %v, but got %v", tc.expected, ruleName)
			}
		}
	}
}
