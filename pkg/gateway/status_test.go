package gateway

import (
	"testing"

	"github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	submarinerv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	fakeversioned "github.com/mkimuram/k8s-ext-connector/pkg/client/clientset/versioned/fake"
	fakev1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/client/clientset/versioned/typed/submariner/v1alpha1/fake"
	"github.com/operator-framework/operator-sdk/pkg/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func compareGateway(t *testing.T, a, b *v1alpha1.Gateway) {
	if a.Status.Conditions[submarinerv1alpha1.ConditionRuleSyncing].Status !=
		b.Status.Conditions[submarinerv1alpha1.ConditionRuleSyncing].Status {
		t.Errorf("RuleSyncing: expected %v, but got %v",
			a.Status.Conditions[submarinerv1alpha1.ConditionRuleSyncing].Status,
			b.Status.Conditions[submarinerv1alpha1.ConditionRuleSyncing].Status)
	}
	if a.Status.Conditions[submarinerv1alpha1.ConditionRuleUpdating].Status !=
		b.Status.Conditions[submarinerv1alpha1.ConditionRuleUpdating].Status {
		t.Errorf("RuleUpdating: expected %v, but got %v",
			a.Status.Conditions[submarinerv1alpha1.ConditionRuleUpdating].Status,
			b.Status.Conditions[submarinerv1alpha1.ConditionRuleUpdating].Status)
	}
	if a.Status.RuleGeneration != b.Status.RuleGeneration {
		t.Errorf("Rulegeneration: expected %v, but got %v", a.Status.RuleGeneration, b.Status.RuleGeneration)
	}
	if a.Status.SyncGeneration != b.Status.SyncGeneration {
		t.Errorf("Rulegeneration: expected %v, but got %v", a.Status.SyncGeneration, b.Status.SyncGeneration)
	}
}

func TestNeedSync(t *testing.T) {
	testCases := []struct {
		name      string
		namespace string
		gw        *v1alpha1.Gateway
		expected  bool
	}{
		{
			name: "Normal case (rule is not updating and generation is different)",
			gw: &v1alpha1.Gateway{
				Status: v1alpha1.GatewayStatus{
					Conditions: status.Conditions{
						submarinerv1alpha1.ConditionRuleSyncing: status.Condition{
							Type:   submarinerv1alpha1.ConditionRuleSyncing,
							Status: corev1.ConditionFalse,
						},
						submarinerv1alpha1.ConditionRuleUpdating: status.Condition{
							Type: submarinerv1alpha1.ConditionRuleUpdating,
							// not updating
							Status: corev1.ConditionFalse,
						},
					},
					// Different generation
					RuleGeneration: 2,
					SyncGeneration: 1,
				},
			},
			expected: true,
		},
		{
			name: "Normal case (rule is not updating and syncing)",
			gw: &v1alpha1.Gateway{
				Status: v1alpha1.GatewayStatus{
					Conditions: status.Conditions{
						submarinerv1alpha1.ConditionRuleSyncing: status.Condition{
							Type: submarinerv1alpha1.ConditionRuleSyncing,
							// Syncing
							Status: corev1.ConditionTrue,
						},
						submarinerv1alpha1.ConditionRuleUpdating: status.Condition{
							Type:   submarinerv1alpha1.ConditionRuleUpdating,
							Status: corev1.ConditionFalse,
						},
					},
					// same generation
					RuleGeneration: 2,
					SyncGeneration: 2,
				},
			},
			expected: true,
		},
		{
			name: "Normal case (rule is updating)",
			gw: &v1alpha1.Gateway{
				Status: v1alpha1.GatewayStatus{
					Conditions: status.Conditions{
						submarinerv1alpha1.ConditionRuleSyncing: status.Condition{
							Type: submarinerv1alpha1.ConditionRuleSyncing,
							// Syncing
							Status: corev1.ConditionTrue,
						},
						submarinerv1alpha1.ConditionRuleUpdating: status.Condition{
							Type: submarinerv1alpha1.ConditionRuleUpdating,
							// Still rule is updating
							Status: corev1.ConditionTrue,
						},
					},
					// different generation
					RuleGeneration: 2,
					SyncGeneration: 1,
				},
			},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)

		needed := needSync(tc.gw)
		if tc.expected != needed {
			t.Errorf("expected %v, but got %v", tc.expected, needed)
		}
	}
}

func TestNeedCheckSync(t *testing.T) {
	testCases := []struct {
		name     string
		gw       *v1alpha1.Gateway
		expected bool
	}{
		{
			name: "Normal case (rule is not updating and generation is the same)",
			gw: &v1alpha1.Gateway{
				Status: v1alpha1.GatewayStatus{
					Conditions: status.Conditions{
						submarinerv1alpha1.ConditionRuleSyncing: status.Condition{
							Type: submarinerv1alpha1.ConditionRuleSyncing,
							// synced
							Status: corev1.ConditionFalse,
						},
						submarinerv1alpha1.ConditionRuleUpdating: status.Condition{
							Type: submarinerv1alpha1.ConditionRuleUpdating,
							// not updating
							Status: corev1.ConditionFalse,
						},
					},
					// same generation
					RuleGeneration: 1,
					SyncGeneration: 1,
				},
			},
			expected: true,
		},
		{
			name: "Normal case (rule is not updating and generation is different)",
			gw: &v1alpha1.Gateway{
				Status: v1alpha1.GatewayStatus{
					Conditions: status.Conditions{
						submarinerv1alpha1.ConditionRuleSyncing: status.Condition{
							Type: submarinerv1alpha1.ConditionRuleSyncing,
							// synced
							Status: corev1.ConditionFalse,
						},
						submarinerv1alpha1.ConditionRuleUpdating: status.Condition{
							Type: submarinerv1alpha1.ConditionRuleUpdating,
							// not updating
							Status: corev1.ConditionFalse,
						},
					},
					// different generation
					RuleGeneration: 2,
					SyncGeneration: 1,
				},
			},
			expected: false,
		},
		{
			name: "Normal case (rule is updating)",
			gw: &v1alpha1.Gateway{
				Status: v1alpha1.GatewayStatus{
					Conditions: status.Conditions{
						submarinerv1alpha1.ConditionRuleSyncing: status.Condition{
							Type: submarinerv1alpha1.ConditionRuleSyncing,
							// synced
							Status: corev1.ConditionFalse,
						},
						submarinerv1alpha1.ConditionRuleUpdating: status.Condition{
							Type: submarinerv1alpha1.ConditionRuleUpdating,
							// updating
							Status: corev1.ConditionTrue,
						},
					},
					// same generation
					RuleGeneration: 1,
					SyncGeneration: 1,
				},
			},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)

		needed := needCheckSync(tc.gw)
		if tc.expected != needed {
			t.Errorf("expected %v, but got %v", tc.expected, needed)
		}
	}
}

func TestSetSyncing(t *testing.T) {
	testCases := []struct {
		name      string
		namespace string
		gw        *v1alpha1.Gateway
		expected  *v1alpha1.Gateway
		expectErr bool
	}{
		{
			name:      "Normal case (Change RuleSyncing false to true)",
			namespace: "ns1",
			gw: &v1alpha1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns1",
					Name:      "gw1",
				},
				Status: v1alpha1.GatewayStatus{
					Conditions: status.Conditions{
						submarinerv1alpha1.ConditionRuleSyncing: status.Condition{
							Type:   submarinerv1alpha1.ConditionRuleSyncing,
							Status: corev1.ConditionFalse,
						},
						submarinerv1alpha1.ConditionRuleUpdating: status.Condition{
							Type:   submarinerv1alpha1.ConditionRuleUpdating,
							Status: corev1.ConditionFalse,
						},
					},
					RuleGeneration: 2,
					SyncGeneration: 1,
				},
			},
			expected: &v1alpha1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns1",
					Name:      "gw1",
				},
				Status: v1alpha1.GatewayStatus{
					Conditions: status.Conditions{
						submarinerv1alpha1.ConditionRuleSyncing: status.Condition{
							Type: submarinerv1alpha1.ConditionRuleSyncing,
							// Only this field wil be changed
							Status: corev1.ConditionTrue,
						},
						submarinerv1alpha1.ConditionRuleUpdating: status.Condition{
							Type:   submarinerv1alpha1.ConditionRuleUpdating,
							Status: corev1.ConditionFalse,
						},
					},
					// No change
					RuleGeneration: 2,
					SyncGeneration: 1,
				},
			},
			expectErr: false,
		},
		{
			name:      "Normal case (RuleSyncing is already true and try to set it to true)",
			namespace: "ns1",
			gw: &v1alpha1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns1",
					Name:      "gw1",
				},
				Status: v1alpha1.GatewayStatus{
					Conditions: status.Conditions{
						submarinerv1alpha1.ConditionRuleSyncing: status.Condition{
							Type: submarinerv1alpha1.ConditionRuleSyncing,
							// Already true
							Status: corev1.ConditionTrue,
						},
						submarinerv1alpha1.ConditionRuleUpdating: status.Condition{
							Type:   submarinerv1alpha1.ConditionRuleUpdating,
							Status: corev1.ConditionFalse,
						},
					},
					RuleGeneration: 2,
					SyncGeneration: 1,
				},
			},
			expected: &v1alpha1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns1",
					Name:      "gw1",
				},
				Status: v1alpha1.GatewayStatus{
					Conditions: status.Conditions{
						submarinerv1alpha1.ConditionRuleSyncing: status.Condition{
							Type: submarinerv1alpha1.ConditionRuleSyncing,
							// Remains true
							Status: corev1.ConditionTrue,
						},
						submarinerv1alpha1.ConditionRuleUpdating: status.Condition{
							Type:   submarinerv1alpha1.ConditionRuleUpdating,
							Status: corev1.ConditionFalse,
						},
					},
					// No change
					RuleGeneration: 2,
					SyncGeneration: 1,
				},
			},
			expectErr: false,
		},
	}

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)

		vcl := fakeversioned.NewSimpleClientset()
		cl := &fakev1alpha1.FakeSubmarinerV1alpha1{Fake: &vcl.Fake}

		// Create tc.gw
		if _, err := cl.Gateways(tc.namespace).Create(tc.gw); err != nil {
			t.Fatalf("creating gw %s failed: %v", tc.gw.Name, err)
		}

		err := setSyncing(cl, tc.namespace, tc.gw)

		gw, err := cl.Gateways(tc.namespace).Get(tc.gw.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("getting gw %s failed: %v", tc.gw.Name, err)
		}

		if tc.expectErr {
			if err == nil {
				t.Errorf("expected error, but got no error")
			}
		} else {
			if err != nil {
				t.Errorf("expected no error, but got %v", err)
			}

			// Compare gw with expected
			compareGateway(t, tc.expected, gw)
		}
	}
}

func TestSetSynced(t *testing.T) {
	testCases := []struct {
		name      string
		namespace string
		gw        *v1alpha1.Gateway
		expected  *v1alpha1.Gateway
		expectErr bool
	}{
		{
			name:      "Normal case (Change RuleSyncing true to false)",
			namespace: "ns1",
			gw: &v1alpha1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns1",
					Name:      "gw1",
				},
				Status: v1alpha1.GatewayStatus{
					Conditions: status.Conditions{
						submarinerv1alpha1.ConditionRuleSyncing: status.Condition{
							Type: submarinerv1alpha1.ConditionRuleSyncing,
							// True
							Status: corev1.ConditionTrue,
						},
						submarinerv1alpha1.ConditionRuleUpdating: status.Condition{
							Type:   submarinerv1alpha1.ConditionRuleUpdating,
							Status: corev1.ConditionFalse,
						},
					},
					RuleGeneration: 2,
					SyncGeneration: 1,
				},
			},
			expected: &v1alpha1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns1",
					Name:      "gw1",
				},
				Status: v1alpha1.GatewayStatus{
					Conditions: status.Conditions{
						submarinerv1alpha1.ConditionRuleSyncing: status.Condition{
							Type: submarinerv1alpha1.ConditionRuleSyncing,
							// Changed to false
							Status: corev1.ConditionFalse,
						},
						submarinerv1alpha1.ConditionRuleUpdating: status.Condition{
							Type:   submarinerv1alpha1.ConditionRuleUpdating,
							Status: corev1.ConditionFalse,
						},
					},
					// Generations are also synced
					RuleGeneration: 2,
					SyncGeneration: 2,
				},
			},
			expectErr: false,
		},
		{
			name:      "Normal case (RuleSyncing is already false and try to set it to false)",
			namespace: "ns1",
			gw: &v1alpha1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns1",
					Name:      "gw1",
				},
				Status: v1alpha1.GatewayStatus{
					Conditions: status.Conditions{
						submarinerv1alpha1.ConditionRuleSyncing: status.Condition{
							Type: submarinerv1alpha1.ConditionRuleSyncing,
							// Already false
							Status: corev1.ConditionFalse,
						},
						submarinerv1alpha1.ConditionRuleUpdating: status.Condition{
							Type:   submarinerv1alpha1.ConditionRuleUpdating,
							Status: corev1.ConditionFalse,
						},
					},
					RuleGeneration: 2,
					SyncGeneration: 2,
				},
			},
			expected: &v1alpha1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns1",
					Name:      "gw1",
				},
				Status: v1alpha1.GatewayStatus{
					Conditions: status.Conditions{
						submarinerv1alpha1.ConditionRuleSyncing: status.Condition{
							Type: submarinerv1alpha1.ConditionRuleSyncing,
							// Remains false
							Status: corev1.ConditionFalse,
						},
						submarinerv1alpha1.ConditionRuleUpdating: status.Condition{
							Type:   submarinerv1alpha1.ConditionRuleUpdating,
							Status: corev1.ConditionFalse,
						},
					},
					// TODO: consider situation that generations need to be upated?
					RuleGeneration: 2,
					SyncGeneration: 2,
				},
			},
			expectErr: false,
		},
	}

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)

		vcl := fakeversioned.NewSimpleClientset()
		cl := &fakev1alpha1.FakeSubmarinerV1alpha1{Fake: &vcl.Fake}

		// Create tc.gw
		if _, err := cl.Gateways(tc.namespace).Create(tc.gw); err != nil {
			t.Fatalf("creating gw %s failed: %v", tc.gw.Name, err)
		}

		err := setSynced(cl, tc.namespace, tc.gw)

		gw, err := cl.Gateways(tc.namespace).Get(tc.gw.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("getting gw %s failed: %v", tc.gw.Name, err)
		}

		if tc.expectErr {
			if err == nil {
				t.Errorf("expected error, but got no error")
			}
		} else {
			if err != nil {
				t.Errorf("expected no error, but got %v", err)
			}

			// Compare gw with expected
			compareGateway(t, tc.expected, gw)
		}
	}
}
