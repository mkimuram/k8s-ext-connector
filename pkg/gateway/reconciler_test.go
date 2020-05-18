package gateway

import (
	"reflect"
	"testing"
	"time"

	"github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	fakeversioned "github.com/mkimuram/k8s-ext-connector/pkg/client/clientset/versioned/fake"
	fakev1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/client/clientset/versioned/typed/submariner/v1alpha1/fake"
)

func TestEnsureSshdRunning(t *testing.T) {
	testCases := []struct {
		name          string
		ip            string
		expectRunning bool
		expectErr     bool
	}{
		{
			name:          "Normal case",
			ip:            "127.0.0.1",
			expectRunning: true,
			expectErr:     false,
		},
	}

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		vcl := fakeversioned.NewSimpleClientset()
		cl := &fakev1alpha1.FakeSubmarinerV1alpha1{Fake: &vcl.Fake}
		g := NewReconciler(cl, "ns1")

		// use func here to defer cancel sshd before waiting for stop
		func() {
			err := g.ensureSshdRunning(tc.ip)
			// Call all cancel functions in r.ssh
			defer g.stopSshd(tc.ip)

			// Ensure sshd to be running
			time.Sleep(time.Millisecond * 100)

			isRunning := g.checkSshdRunning(tc.ip)

			if tc.expectErr {
				if err == nil {
					t.Errorf("expected error, but got no error")
				}
			} else {
				if tc.expectRunning != isRunning {
					t.Errorf("expectedRunning:%v, but got isRunning:%v", tc.expectRunning, isRunning)
				}
			}

		}()
	}
}

func TestGetExpectedIptablesRule(t *testing.T) {
	testCases := []struct {
		name               string
		gw                 *v1alpha1.Gateway
		expectedJumpChains map[string][][]string
		expectedChains     map[string][][]string
		expectErr          bool
	}{
		{
			name: "Normal case",
			gw: &v1alpha1.Gateway{
				Spec: v1alpha1.GatewaySpec{
					EgressRules: []v1alpha1.GatewayRule{
						{
							Protocol:        "TCP",
							SourceIP:        "10.244.0.12",
							TargetPort:      "8000",
							DestinationPort: "8001",
							DestinationIP:   "192.168.122.139",
							Forwarder: v1alpha1.ForwarderRef{
								Namespace: "fwd1",
								Name:      "ns1",
							},
							ForwarderIP: "10.244.0.157",
							RelayPort:   "2050",
						},
					},
					IngressRules: []v1alpha1.GatewayRule{
						{
							Protocol:        "TCP",
							SourceIP:        "192.168.122.139",
							TargetPort:      "80",
							DestinationPort: "80",
							DestinationIP:   "10.104.205.241",
							Forwarder: v1alpha1.ForwarderRef{
								Namespace: "fwd1",
								Name:      "ns1",
							},
							ForwarderIP: "10.244.0.157",
							RelayPort:   "2049",
						},
					},
					GatewayIP: "192.168.122.201",
				},
			},
			expectedJumpChains: map[string][][]string{
				"PREROUTING":  [][]string{{"-j", "prec0a87ac9"}},
				"POSTROUTING": [][]string{{"-j", "pstc0a87ac9"}},
			},
			expectedChains: map[string][][]string{
				"prec0a87ac9": [][]string{{"-m", "tcp", "-p", "tcp", "--dst", "192.168.122.201", "--src", "192.168.122.139", "--dport", "80", "-j", "DNAT", "--to-destination", "192.168.122.201:2049"}},
				"pstc0a87ac9": [][]string{{"-m", "tcp", "-p", "tcp", "--dst", "10.104.205.241", "--dport", "2049", "-j", "SNAT", "--to-source", "192.168.122.201"}},
			},
			expectErr: false,
		},
		{
			name: "Error case (invalid gateway IP)",
			gw: &v1alpha1.Gateway{
				Spec: v1alpha1.GatewaySpec{
					EgressRules: []v1alpha1.GatewayRule{
						{
							Protocol:        "TCP",
							SourceIP:        "10.244.0.12",
							TargetPort:      "8000",
							DestinationPort: "8001",
							DestinationIP:   "192.168.122.139",
							Forwarder: v1alpha1.ForwarderRef{
								Namespace: "fwd1",
								Name:      "ns1",
							},
							ForwarderIP: "10.244.0.157",
							RelayPort:   "2050",
						},
					},
					IngressRules: []v1alpha1.GatewayRule{
						{
							Protocol:        "TCP",
							SourceIP:        "192.168.122.139",
							TargetPort:      "80",
							DestinationPort: "80",
							DestinationIP:   "10.104.205.241",
							Forwarder: v1alpha1.ForwarderRef{
								Namespace: "fwd1",
								Name:      "ns1",
							},
							ForwarderIP: "10.244.0.157",
							RelayPort:   "2049",
						},
					},
					// Invalid GatewayIP
					GatewayIP: "",
				},
			},
			expectedJumpChains: map[string][][]string{},
			expectedChains:     map[string][][]string{},
			expectErr:          true,
		},
	}

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)

		jumpChains, chains, err := getExpectedIptablesRule(tc.gw)
		if tc.expectErr {
			if err == nil {
				t.Errorf("expected error, but got no error")
			}
		} else {
			if !reflect.DeepEqual(tc.expectedJumpChains, jumpChains) {
				t.Errorf("expected %v, but got %v", tc.expectedJumpChains, jumpChains)
			}
			if !reflect.DeepEqual(tc.expectedChains, chains) {
				t.Errorf("expected %v, but got %v", tc.expectedChains, chains)
			}
		}
	}
}
