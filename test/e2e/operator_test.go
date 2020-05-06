package e2e

import (
	"testing"
	"time"

	goctx "context"

	"github.com/mkimuram/k8s-ext-connector/pkg/apis"
	"github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/operator-framework/operator-sdk/pkg/test/e2eutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var (
	retryInterval        = time.Second * 5
	timeout              = time.Second * 60
	cleanupRetryInterval = time.Second * 1
	cleanupTimeout       = time.Second * 5
)

func TestOperator(t *testing.T) {
	// Add CRDs to scheme
	// ExternealService
	if err := framework.AddToFrameworkScheme(apis.AddToScheme, &v1alpha1.ExternalServiceList{}); err != nil {
		t.Fatalf("failed to add ExternalService scheme to framework: %v", err)
	}
	// Forwarder
	if err := framework.AddToFrameworkScheme(apis.AddToScheme, &v1alpha1.ForwarderList{}); err != nil {
		t.Fatalf("failed to add Forwarder scheme to framework: %v", err)
	}
	// Gateway
	if err := framework.AddToFrameworkScheme(apis.AddToScheme, &v1alpha1.GatewayList{}); err != nil {
		t.Fatalf("failed to add Gateway scheme to framework: %v", err)
	}

	// Create test context
	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup()

	// Create sa, clusterrole, clusterrolebinding, and operator
	// Because operator is cluster-scoped, operator needs to be created in externa-services namesapce.
	if err := ctx.InitializeClusterResources(&framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval}); err != nil {
		t.Fatalf("failed to initialize cluster resources: %v", err)
	}

	// Get namespace
	namespace, err := ctx.GetNamespace()
	if err != nil {
		t.Fatal(err)
	}
	// Get global framework variables
	f := framework.Global

	// Wait for operator to be ready
	if err := e2eutil.WaitForOperatorDeployment(t, f.KubeClient, namespace, "k8s-ext-connector", 1, retryInterval, timeout); err != nil {
		t.Fatal(err)
	}

	if err := resourceTest(t, f, ctx); err != nil {
		t.Fatal(err)
	}

}

func resourceTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) error {
	namespace, err := ctx.GetNamespace()
	if err != nil {
		t.Fatal(err)
	}

	es := &v1alpha1.ExternalService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "es1",
			Namespace: namespace,
		},
		Spec: v1alpha1.ExternalServiceSpec{
			TargetIP: "192.168.122.139",
			Sources: []v1alpha1.Source{
				{
					Service: v1alpha1.ServiceRef{
						Name:      "svc1",
						Namespace: namespace,
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

	// Create external service
	err = f.Client.Create(goctx.TODO(), es, &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	if err != nil {
		return err
	}

	time.Sleep(time.Second * 10)

	// Confirm that expected resources are created
	pod := &corev1.Pod{}
	err = f.Client.Get(goctx.TODO(), types.NamespacedName{Name: es.Name, Namespace: namespace}, pod)
	if err != nil {
		return err
	}
	svc := &corev1.Service{}
	err = f.Client.Get(goctx.TODO(), types.NamespacedName{Name: es.Name, Namespace: namespace}, svc)
	if err != nil {
		return err
	}
	fwd := &v1alpha1.Forwarder{}
	err = f.Client.Get(goctx.TODO(), types.NamespacedName{Name: es.Name, Namespace: namespace}, fwd)
	if err != nil {
		return err
	}

	return nil
}
