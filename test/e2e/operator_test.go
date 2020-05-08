package e2e

import (
	goctx "context"
	"testing"
	"time"

	"github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var _ = ginkgo.Describe("[k8s-ext-connector]", func() {
	var (
		ctx *framework.TestCtx
		f   *framework.Framework
		t   *testing.T
		ns  string
	)
	ginkgo.BeforeEach(func() {
		t = Testing
		f = framework.Global
		ctx, ns = initOperator(t, operatorName, objs)
	})

	ginkgo.AfterEach(func() {
		ctx.Cleanup()
	})

	ginkgo.It("should create resources when externalService is created", func() {
		createExternalService(t, f, ctx, ns, true /* checkResource */)
	})

	ginkgo.It("should connect to external server from pod via specified source IP", func() {
		createExternalService(t, f, ctx, ns, false /* checkResource */)
		createSourcePodSvc(t, f, ctx, ns)
	})
})

func cleanupOptions(ctx *framework.TestCtx) *framework.CleanupOptions {
	return &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval}
}

func createExternalService(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, ns string, checkResource bool) {
	var err error

	// Create externalService
	es := &v1alpha1.ExternalService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "es1",
			Namespace: ns,
		},
		Spec: v1alpha1.ExternalServiceSpec{
			TargetIP: "192.168.122.139",
			Sources: []v1alpha1.Source{
				{
					Service: v1alpha1.ServiceRef{
						Name:      "svc1",
						Namespace: ns,
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

	err = f.Client.Create(goctx.TODO(), es, cleanupOptions(ctx))
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "creating externalservice failed")

	if checkResource {
		time.Sleep(time.Second)

		// Confirm that expected resources are created
		pod := &corev1.Pod{}
		err = f.Client.Get(goctx.TODO(), types.NamespacedName{Name: es.Name, Namespace: ns}, pod)
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "pod isn't created")

		svc := &corev1.Service{}
		err = f.Client.Get(goctx.TODO(), types.NamespacedName{Name: es.Name, Namespace: ns}, svc)
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "service isn't created")

		fwd := &v1alpha1.Forwarder{}
		err = f.Client.Get(goctx.TODO(), types.NamespacedName{Name: es.Name, Namespace: ns}, fwd)
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "forwarder isn't created")
	}
}

func createSourcePodSvc(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, ns string) {
	var err error

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: ns,
			Labels:    map[string]string{"label1": "val1"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "centos",
					Image:   "centos:7",
					Command: []string{"python", "-m", "SimpleHTTPServer", "80"},
				},
			},
		},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc1",
			Namespace: ns,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Protocol: corev1.ProtocolTCP,
					Port:     80,
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 80,
					},
				},
			},
			Selector: map[string]string{"label1": "val1"},
		},
	}

	err = f.Client.Create(goctx.TODO(), pod, cleanupOptions(ctx))
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "creating source pod failed")

	err = f.Client.Create(goctx.TODO(), svc, cleanupOptions(ctx))
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "creating source service failed")
}
