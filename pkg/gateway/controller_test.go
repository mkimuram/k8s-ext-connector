package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	submarinerv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	fakeversioned "github.com/mkimuram/k8s-ext-connector/pkg/client/clientset/versioned/fake"
	fakev1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/client/clientset/versioned/typed/submariner/v1alpha1/fake"
	"github.com/operator-framework/operator-sdk/pkg/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type FakeGatewaySyncer struct {
}

var _ GatewaySyncerInterface = &FakeGatewaySyncer{}

func (g *FakeGatewaySyncer) syncRule(gw *v1alpha1.Gateway) error {
	// Always succeeds
	return nil
}

func (g *FakeGatewaySyncer) ruleSynced(gw *v1alpha1.Gateway) bool {
	// Always return true
	return true
}

func TestNeedEnqueue(t *testing.T) {
	testCases := []struct {
		name      string
		namespace string
		obj       interface{}
		expected  bool
	}{
		{
			name:      "Normal case (namespace match)",
			namespace: "ns1",
			obj: &v1alpha1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					// nampspace match
					Namespace: "ns1",
					Name:      "gw1",
				},
			},
			expected: true,
		},
		{
			name:      "Normal case (namespace unmatch)",
			namespace: "ns1",
			obj: &v1alpha1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					// nampspace unmatch
					Namespace: "ns2",
					Name:      "gw1",
				},
			},
			expected: false,
		},
		{
			name:      "Normal case (no namespace)",
			namespace: "ns1",
			obj: &v1alpha1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					// no nampspace
					Name: "gw1",
				},
			},
			expected: false,
		},
		{
			name:      "Normal case (not a gateway CR)",
			namespace: "ns1",
			// Not a gateway CR
			obj: &v1alpha1.Forwarder{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns1",
					Name:      "gw1",
				},
			},
			expected: false,
		},
		{
			name:      "Normal case (not a metadata object)",
			namespace: "ns1",
			obj:       "string",
			expected:  false,
		},
	}

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		vcl := fakeversioned.NewSimpleClientset()
		cl := &fakev1alpha1.FakeSubmarinerV1alpha1{Fake: &vcl.Fake}
		controller := NewGatewayController(cl, vcl, tc.namespace, &FakeGatewaySyncer{})

		needed := controller.needEnqueue(tc.obj)
		if tc.expected != needed {
			t.Errorf("expected %v, but got %v", tc.expected, needed)
		}
	}
}

func TestEnqueueGateway(t *testing.T) {
	testCases := []struct {
		name      string
		namespace string
		obj       interface{}
	}{
		{
			name:      "Normal case (one gateway CRD)",
			namespace: "ns1",
			obj: &v1alpha1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns1",
					Name:      "gw1",
				},
			},
		},
		{
			name:      "Error case (not a gateway CRD)",
			namespace: "ns1",
			obj:       "string",
		},
	}

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		vcl := fakeversioned.NewSimpleClientset()
		cl := &fakev1alpha1.FakeSubmarinerV1alpha1{Fake: &vcl.Fake}
		controller := NewGatewayController(cl, vcl, tc.namespace, &FakeGatewaySyncer{})

		controller.enqueueGateway(tc.obj)
		// TODO: consider checking if it work correctly
		// Currently, it just logs error message in error case
	}
}

func TestGetKey(t *testing.T) {
	testCases := []struct {
		name     string
		obj      interface{}
		expected string
	}{
		{
			name: "Normal case (gateway CRD with namespace)",
			obj: &v1alpha1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns1",
					Name:      "gw1",
				},
			},
			expected: "ns1/gw1",
		},
		{
			name: "Normal case (one gateway without namespace)",
			obj: &v1alpha1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					// No namespace
					Name: "gw1",
				},
			},
			expected: "gw1",
		},
		{
			name: "Error case (not a gateway CRD)",
			obj:  "string",
			// TODO: Consider returning error?
			expected: "",
		},
	}

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		key := getKey(tc.obj)
		if tc.expected != key {
			t.Errorf("expected %v, but got %v", tc.expected, key)
		}
	}
}

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

func TestRun(t *testing.T) {
	testCases := []struct {
		name      string
		namespace string
		gw        *v1alpha1.Gateway
		expected  *v1alpha1.Gateway
	}{
		{
			name:      "Normal case (Not updating rule should be synced if generations are different)",
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
					// Different generation
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
							Type:   submarinerv1alpha1.ConditionRuleSyncing,
							Status: corev1.ConditionFalse,
						},
						submarinerv1alpha1.ConditionRuleUpdating: status.Condition{
							Type:   submarinerv1alpha1.ConditionRuleUpdating,
							Status: corev1.ConditionFalse,
						},
					},
					// Same generation
					RuleGeneration: 2,
					SyncGeneration: 2,
				},
			},
		},
		{
			name:      "Normal case (Not updating rule should be synced if generations are different)",
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
							// Syncing
							Status: corev1.ConditionTrue,
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
							// Synced
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
		},
		{
			name:      "Normal case (Gateway won't updated if rule is still updating)",
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
							// Still rule is updating
							Type:   submarinerv1alpha1.ConditionRuleUpdating,
							Status: corev1.ConditionTrue,
						},
					},
					// Different generation
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
							Type:   submarinerv1alpha1.ConditionRuleSyncing,
							Status: corev1.ConditionFalse,
						},
						submarinerv1alpha1.ConditionRuleUpdating: status.Condition{
							Type: submarinerv1alpha1.ConditionRuleUpdating,
							// Still rule is updating
							Status: corev1.ConditionTrue,
						},
					},
					// Different generation
					RuleGeneration: 2,
					SyncGeneration: 1,
				},
			},
		},
		// TODO: Add more test (may need to use mock for syncer to test error cases)
	}

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		vcl := fakeversioned.NewSimpleClientset()
		cl := &fakev1alpha1.FakeSubmarinerV1alpha1{Fake: &vcl.Fake}
		controller := NewGatewayController(cl, vcl, tc.namespace, &FakeGatewaySyncer{})

		// Call controller run
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			select {
			case <-ctx.Done():
				return
			default:
				controller.Run()
			}
		}()

		// Create tc.gw
		if _, err := controller.clientset.Gateways(tc.namespace).Create(tc.gw); err != nil {
			t.Fatalf("creating gw %v failed: %v", tc.gw, err)
		}

		// Sleep for controller to process added gw
		time.Sleep(time.Second)

		// Get gw that should be modified by controller
		gw, err := controller.clientset.Gateways(tc.namespace).Get(tc.gw.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("getting gw %v failed: %v", tc.gw, err)
		}

		// Compare gw with expected
		compareGateway(t, tc.expected, gw)
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
			t.Fatalf("creating gw %v failed: %v", tc.gw, err)
		}

		err := setSyncing(cl, tc.namespace, tc.gw)

		gw, err := cl.Gateways(tc.namespace).Get(tc.gw.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("getting gw %v failed: %v", tc.gw, err)
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
			t.Fatalf("creating gw %v failed: %v", tc.gw, err)
		}

		err := setSynced(cl, tc.namespace, tc.gw)

		gw, err := cl.Gateways(tc.namespace).Get(tc.gw.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("getting gw %v failed: %v", tc.gw, err)
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
