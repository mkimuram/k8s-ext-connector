package externalservice

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	submarinerv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	es = &v1alpha1.ExternalService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "es1",
			Namespace: "ns1",
		},
		Spec: v1alpha1.ExternalServiceSpec{
			TargetIP: "192.168.122.139",
			Sources: []v1alpha1.Source{
				{
					Service: v1alpha1.ServiceRef{
						Name:      "svc1",
						Namespace: "ns1",
					},
					SourceIP: "192.168.122.200",
				},
			},
			Ports: []corev1.ServicePort{
				{
					Protocol: corev1.ProtocolTCP,
					Port:     80,
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 8080,
					},
				},
			},
		},
	}
	fwdPodWithIP = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "es1",
			Namespace: "external-services",
			Labels: map[string]string{
				ExternalServiceNamespaceLabel: "ns1",
				ExternalServiceNameLabel:      "es1",
			},
		},
		Status: corev1.PodStatus{
			PodIP: "10.0.0.3",
		},
	}
	fwdSvcWithIP = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "es1",
			Namespace: "external-services",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Protocol: corev1.ProtocolTCP,
					Port:     80,
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 8080,
					},
				},
			},
			ClusterIP: "10.10.0.5",
		},
	}
	svc = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc1",
			Namespace: "ns1",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Protocol: corev1.ProtocolTCP,
					Port:     8443,
				},
			},
			ClusterIP: "10.20.0.8",
		},
	}
	ep = &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc1",
			Namespace: "ns1",
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{IP: "10.0.0.4"},
				},
			},
		},
	}
	emptyRuleFwd = &v1alpha1.Forwarder{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "es1",
			Namespace: "external-services",
		},
		Spec: v1alpha1.ForwarderSpec{
			EgressRules:  []v1alpha1.ForwarderRule{},
			IngressRules: []v1alpha1.ForwarderRule{},
			ForwarderIP:  "10.10.0.5",
		},
	}
	fwd = &v1alpha1.Forwarder{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "es1",
			Namespace: "external-services",
		},
		Spec: v1alpha1.ForwarderSpec{
			EgressRules: []v1alpha1.ForwarderRule{
				{
					Protocol:        "TCP",
					SourceIP:        "10.0.0.4",
					TargetPort:      "8080",
					DestinationPort: "80",
					DestinationIP:   "192.168.122.139",
					Gateway: v1alpha1.GatewayRef{
						Namespace: "external-services",
						Name:      "gwrulec0a87ac8",
					},
					GatewayIP: "192.168.122.200",
					RelayPort: "2049",
				},
			},
			IngressRules: []v1alpha1.ForwarderRule{
				{
					Protocol:        "TCP",
					SourceIP:        "192.168.122.139",
					TargetPort:      "8443",
					DestinationPort: "8443",
					DestinationIP:   "10.20.0.8",
					Gateway: v1alpha1.GatewayRef{
						Namespace: "external-services",
						Name:      "gwrulec0a87ac8",
					},
					GatewayIP: "192.168.122.200",
					RelayPort: "2049",
				},
			},
			ForwarderIP: "10.0.0.3",
		},
	}
	gw = &v1alpha1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gwrulec0a87ac8",
			Namespace: "external-services",
		},
		Spec: v1alpha1.GatewaySpec{
			EgressRules: []v1alpha1.GatewayRule{
				{
					Protocol:        "TCP",
					SourceIP:        "10.0.0.4",
					TargetPort:      "8080",
					DestinationPort: "80",
					DestinationIP:   "192.168.122.139",
					Forwarder: v1alpha1.ForwarderRef{
						Namespace: "external-services",
						Name:      "es1",
					},
					ForwarderIP: "10.0.0.3",
					RelayPort:   "2049",
				},
			},
			IngressRules: []v1alpha1.GatewayRule{
				{
					Protocol:        "TCP",
					SourceIP:        "192.168.122.139",
					TargetPort:      "8443",
					DestinationPort: "8443",
					DestinationIP:   "10.20.0.8",
					Forwarder: v1alpha1.ForwarderRef{
						Namespace: "external-services",
						Name:      "es1",
					},
					ForwarderIP: "10.0.0.3",
					RelayPort:   "2049",
				},
			},
			GatewayIP: "192.168.122.200",
		},
	}
)

func compareForwarder(a, b *v1alpha1.Forwarder) error {
	if a.ObjectMeta.Namespace != b.ObjectMeta.Namespace || a.ObjectMeta.Name != b.ObjectMeta.Name {
		return fmt.Errorf("Metadata are different between %#v and %#v", a.ObjectMeta, b.ObjectMeta)
	}
	if !reflect.DeepEqual(a.Spec.IngressRules, b.Spec.IngressRules) {
		return fmt.Errorf("IngressRules are different between %#v and %#v", a.Spec.IngressRules, b.Spec.IngressRules)
	}
	if !reflect.DeepEqual(a.Spec.EgressRules, b.Spec.EgressRules) {
		return fmt.Errorf("EgressRules are different between %#v and %#v", a.Spec.EgressRules, b.Spec.EgressRules)
	}
	return nil
}

func compareGateway(a, b *v1alpha1.Gateway) error {
	if a.ObjectMeta.Namespace != b.ObjectMeta.Namespace || a.ObjectMeta.Name != b.ObjectMeta.Name {
		return fmt.Errorf("Metadata are different between %#v and %#v", a.ObjectMeta, b.ObjectMeta)
	}
	if !reflect.DeepEqual(a.Spec.IngressRules, b.Spec.IngressRules) {
		return fmt.Errorf("IngressRules are different between %#v and %#v", a.Spec.IngressRules, b.Spec.IngressRules)
	}
	if !reflect.DeepEqual(a.Spec.EgressRules, b.Spec.EgressRules) {
		return fmt.Errorf("EgressRules are different between %#v and %#v", a.Spec.EgressRules, b.Spec.EgressRules)
	}
	return nil
}

func TestReconcile(t *testing.T) {
	testCases := []struct {
		name        string
		req         reconcile.Request
		objs        []runtime.Object
		expected    reconcile.Result
		expectedErr error
		expectedFwd *v1alpha1.Forwarder
		expectedGw  *v1alpha1.Gateway
	}{
		{
			name: "Error case (Fails but not requeued, due to lack of external service)",
			req: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "ns1",
					Name:      "es1",
				},
			},
			objs:        []runtime.Object{},
			expected:    reconcile.Result{},
			expectedErr: nil,
			expectedFwd: nil,
			expectedGw:  nil,
		},
		{
			name: "Error case (Fails and requeued, due to no IP assigned to forwarder pod)",
			req: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "ns1",
					Name:      "es1",
				},
			},
			objs:     []runtime.Object{es},
			expected: reconcile.Result{},
			// Return error because no podIP assigned in fake client
			expectedErr: fmt.Errorf("forwarder pod has no IP address assigned"),
			expectedFwd: nil,
			expectedGw:  nil,
		},
		{
			name: "Normal case (no service exists)",
			req: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "ns1",
					Name:      "es1",
				},
			},
			objs:        []runtime.Object{es, fwdPodWithIP},
			expected:    reconcile.Result{},
			expectedErr: nil,
			expectedFwd: emptyRuleFwd,
			expectedGw:  nil,
		},
		{
			name: "Normal case (service exists)",
			req: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "ns1",
					Name:      "es1",
				},
			},
			objs:        []runtime.Object{es, fwdPodWithIP, fwdSvcWithIP, svc, ep},
			expected:    reconcile.Result{},
			expectedErr: nil,
			expectedFwd: fwd,
			expectedGw:  gw,
		},
	}

	s := runtime.NewScheme()
	corev1.AddToScheme(s)
	v1alpha1.AddToScheme(s)

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)

		cl := fake.NewFakeClientWithScheme(s, tc.objs...)
		r := &ReconcileExternalService{client: cl, scheme: s}

		result, err := r.Reconcile(tc.req)

		if !reflect.DeepEqual(tc.expectedErr, err) {
			t.Errorf("expected err:%v , but got err:%v", tc.expectedErr, err)
		}
		if !reflect.DeepEqual(tc.expected, result) {
			t.Errorf("expected:%v, but got:%v", tc.expected, result)
		}

		// Check forwarder
		if tc.expectedFwd != nil {
			fwd := &submarinerv1alpha1.Forwarder{}
			if err := cl.Get(context.TODO(), types.NamespacedName{Namespace: tc.expectedFwd.Namespace, Name: tc.expectedFwd.Name}, fwd); err != nil {
				t.Fatalf("failed to get forwarder")
			}
			if err := compareForwarder(tc.expectedFwd, fwd); err != nil {
				t.Errorf("%v", err)
			}
		}

		if tc.expectedGw != nil {
			gw := &submarinerv1alpha1.Gateway{}
			if err := cl.Get(context.TODO(), types.NamespacedName{Namespace: tc.expectedGw.Namespace, Name: tc.expectedGw.Name}, gw); err != nil {
				t.Fatalf("failed to get gateway")
			}

			if err := compareGateway(tc.expectedGw, gw); err != nil {
				t.Errorf("%v", err)
			}
		}
	}
}
