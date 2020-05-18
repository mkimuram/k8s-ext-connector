package forwarder

import (
	"reflect"
	"testing"

	"github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
)

func TestGetExpectedSSHTunnel(t *testing.T) {
	testCases := []struct {
		name     string
		fwd      *v1alpha1.Forwarder
		expected map[string]bool
	}{
		{
			name: "Normal case",
			fwd: &v1alpha1.Forwarder{
				Spec: v1alpha1.ForwarderSpec{
					EgressRules: []v1alpha1.ForwarderRule{
						{
							Protocol:        "TCP",
							SourceIP:        "10.244.0.12",
							TargetPort:      "8000",
							DestinationPort: "8001",
							DestinationIP:   "192.168.122.139",
							Gateway: v1alpha1.GatewayRef{
								Namespace: "ns1",
								Name:      "gw1",
							},
							GatewayIP: "192.168.122.200",
							RelayPort: "2049",
						},
					},
					ForwarderIP: "10.0.0.2",
				},
			},
			expected: map[string]bool{
				"10.0.0.2:2049:192.168.122.200:2022:192.168.122.139:8001": true,
			},
		},
	}

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		sshTunnel := getExpectedSSHTunnel(tc.fwd)

		if !reflect.DeepEqual(tc.expected, sshTunnel) {
			t.Errorf("expected:%v, but got:%v", tc.expected, sshTunnel)
		}
	}
}

func TestGetExpectedRemoteSSHTunnel(t *testing.T) {
	testCases := []struct {
		name     string
		fwd      *v1alpha1.Forwarder
		expected map[string]bool
	}{
		{
			name: "Normal case",
			fwd: &v1alpha1.Forwarder{
				Spec: v1alpha1.ForwarderSpec{
					IngressRules: []v1alpha1.ForwarderRule{
						{
							Protocol:        "TCP",
							SourceIP:        "192.168.122.139",
							TargetPort:      "80",
							DestinationPort: "80",
							DestinationIP:   "10.104.205.241",
							Gateway: v1alpha1.GatewayRef{
								Namespace: "ns1",
								Name:      "gw1",
							},
							GatewayIP: "192.168.122.200",
							RelayPort: "2050",
						},
					},
					ForwarderIP: "10.0.0.2",
				},
			},
			expected: map[string]bool{
				"10.104.205.241:80:192.168.122.200:2022:192.168.122.200:2050": true,
			},
		},
	}

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		sshTunnel := getExpectedRemoteSSHTunnel(tc.fwd)

		if !reflect.DeepEqual(tc.expected, sshTunnel) {
			t.Errorf("expected:%v, but got:%v", tc.expected, sshTunnel)
		}
	}
}
func TestGetExpectedIptablesRule(t *testing.T) {
	testCases := []struct {
		name     string
		fwd      *v1alpha1.Forwarder
		expected map[string][][]string
	}{
		{
			name: "Normal case",
			fwd: &v1alpha1.Forwarder{
				Spec: v1alpha1.ForwarderSpec{
					EgressRules: []v1alpha1.ForwarderRule{
						{
							Protocol:        "TCP",
							SourceIP:        "10.244.0.12",
							TargetPort:      "8000",
							DestinationPort: "8001",
							DestinationIP:   "192.168.122.139",
							Gateway: v1alpha1.GatewayRef{
								Namespace: "ns1",
								Name:      "gw1",
							},
							GatewayIP: "192.168.122.200",
							RelayPort: "2049",
						},
					},
					ForwarderIP: "10.0.0.2",
				},
			},
			expected: map[string][][]string{
				"PREROUTING": [][]string{
					{"-m", "tcp", "-p", "tcp", "--dst", "10.0.0.2", "--src", "10.244.0.12", "--dport", "8000", "-j", "DNAT", "--to-destination", "10.0.0.2:2049"},
				},
				"POSTROUTING": [][]string{
					{"-m", "tcp", "-p", "tcp", "--dst", "192.168.122.139", "--dport", "2049", "-j", "SNAT", "--to-source", "10.0.0.2"},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		rules := getExpectedIptablesRule(tc.fwd)

		if !reflect.DeepEqual(tc.expected, rules) {
			t.Errorf("expected:%v, but got:%v", tc.expected, rules)
		}
	}
}
