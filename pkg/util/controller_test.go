package util

import (
	"context"
	"testing"
	"time"

	"github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	fakeversioned "github.com/mkimuram/k8s-ext-connector/pkg/client/clientset/versioned/fake"
	fakev1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/client/clientset/versioned/typed/submariner/v1alpha1/fake"
	sbinformers "github.com/mkimuram/k8s-ext-connector/pkg/client/informers/externalversions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

type FakeReconciler struct {
}

var _ ReconcilerInterface = &FakeReconciler{}

func (g *FakeReconciler) Reconcile(namespace, name string) error {
	// Always succeeds
	return nil
}

func newFakeController() *Controller {
	vcl := fakeversioned.NewSimpleClientset()
	cl := &fakev1alpha1.FakeSubmarinerV1alpha1{Fake: &vcl.Fake}
	informerFactory := sbinformers.NewSharedInformerFactory(vcl, time.Second*30)
	informer := informerFactory.Submariner().V1alpha1().Gateways().Informer()
	return NewController(cl, informerFactory, informer, &FakeReconciler{})
}

func TestEnqueue(t *testing.T) {
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
		controller := newFakeController()

		controller.enqueue(tc.obj)
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
			name: "Error case (not a CR)",
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

func TestRun(t *testing.T) {
	testCases := []struct {
		name            string
		namespace       string
		obj             interface{}
		objType         string
		informerObjType string
		expectReconcile bool
	}{
		{
			name:      "Normal case (gateway CRD is added)",
			namespace: "ns1",
			obj: &v1alpha1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns1",
					Name:      "gw1",
				},
			},
			objType:         "Gateways",
			informerObjType: "Gateways",
			expectReconcile: true,
		},
		{
			name:      "Normal case (forwarder CRD is added)",
			namespace: "ns1",
			obj: &v1alpha1.Forwarder{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns1",
					Name:      "fwd1",
				},
			},
			objType:         "Forwarders",
			informerObjType: "Forwarders",
			expectReconcile: true,
		},
		{
			name:      "Normal case (gateway CRD is added)",
			namespace: "ns1",
			obj: &v1alpha1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns1",
					Name:      "gw1",
				},
			},
			objType:         "Gateways",
			informerObjType: "Forwarders",
			// no reconcile call expected because objType doesn't match
			expectReconcile: false,
		},
	}

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)

		// Prepare controller
		var informer cache.SharedIndexInformer
		vcl := fakeversioned.NewSimpleClientset()
		cl := &fakev1alpha1.FakeSubmarinerV1alpha1{Fake: &vcl.Fake}
		informerFactory := sbinformers.NewSharedInformerFactory(vcl, time.Second*30)
		if tc.informerObjType == "Gateways" {
			informer = informerFactory.Submariner().V1alpha1().Gateways().Informer()
		} else if tc.informerObjType == "Forwarders" {
			informer = informerFactory.Submariner().V1alpha1().Forwarders().Informer()
		} else {
			t.Fatalf("invalid objType %s specified", tc.objType)
		}
		controller := NewController(cl, informerFactory, informer, &FakeReconciler{})

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

		// Create resource
		if tc.objType == "Gateways" {
			if _, err := controller.clientset.Gateways(tc.namespace).Create(tc.obj.(*v1alpha1.Gateway)); err != nil {
				t.Fatalf("creating gateway %v failed: %v", tc.obj, err)
			}
		} else if tc.objType == "Forwarders" {
			if _, err := controller.clientset.Forwarders(tc.namespace).Create(tc.obj.(*v1alpha1.Forwarder)); err != nil {
				t.Fatalf("creating forwarder %v failed: %v", tc.obj, err)
			}
		}

		// Sleep for controller to process added resource
		time.Sleep(time.Second)

		// TODO: Check if reconcile is called
	}
}
