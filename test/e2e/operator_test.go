package e2e

import (
	goctx "context"
	"fmt"
	"os/exec"
	"strconv"
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
		es := &v1alpha1.ExternalService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "es1",
				Namespace: ns,
			},
			Spec: v1alpha1.ExternalServiceSpec{
				TargetIP: "192.168.33.254",
				Sources: []v1alpha1.Source{
					{
						Service: v1alpha1.ServiceRef{
							Name:      "svc1",
							Namespace: ns,
						},
						SourceIP: "192.168.33.100",
					},
				},
				Ports: []corev1.ServicePort{
					{
						Protocol: corev1.ProtocolTCP,
						Port:     8888,
						TargetPort: intstr.IntOrString{
							Type:   intstr.Int,
							IntVal: 8888,
						},
					},
				},
			},
		}
		createExternalService(t, f, ctx, ns, es, true /* checkResource */)
	})

	ginkgo.Context("[with servers]", func() {
		var (
			gctx     goctx.Context
			cancel   goctx.CancelFunc
			rsrvAddr string
			esSrcIP  string
			rsrvPort = 8888
			esPort   = 8888
			es       *v1alpha1.ExternalService
		)

		ginkgo.BeforeEach(func() {
			if len(ips) < 2 {
				fmt.Fprintf(ginkgo.GinkgoWriter, "ips: %v\n", ips)
				ginkgo.Skip("Skipping tests with servers because not enough IPs assigned")
			}
			rsrvAddr = ips[0]
			esSrcIP = ips[1]

			// Set variables to es
			es = &v1alpha1.ExternalService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "es1",
					Namespace: ns,
				},
				Spec: v1alpha1.ExternalServiceSpec{
					TargetIP: rsrvAddr,
					Sources: []v1alpha1.Source{
						{
							Service: v1alpha1.ServiceRef{
								Name:      "svc1",
								Namespace: ns,
							},
							SourceIP: esSrcIP,
						},
					},
					Ports: []corev1.ServicePort{
						{
							Protocol: corev1.ProtocolTCP,
							Port:     int32(rsrvPort),
							TargetPort: intstr.IntOrString{
								Type:   intstr.Int,
								IntVal: int32(esPort),
							},
						},
					},
				},
			}

			gctx, cancel = goctx.WithCancel(goctx.Background())

			runGateway(gctx)
			runRemoteServer(gctx, rsrvAddr, strconv.Itoa(rsrvPort))
		})

		ginkgo.AfterEach(func() {
			cancel()
		})

		ginkgo.It("should connect to external server from pod via specified source IP", func() {
			createExternalService(t, f, ctx, ns, es, true /* checkResource */)
			createSourcePodSvc(t, f, ctx, ns)
			// wait for pod to be ready
			time.Sleep(time.Second * 5)

			// access from this host to remote server
			cmd := exec.Command("curl", fmt.Sprintf("%s:%d", rsrvAddr, rsrvPort))
			out, err := cmd.Output()
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "error executing command")
			// expect host IP (just check that it isn't podIP and externalservice's source IP)
			gomega.Expect(out).NotTo(gomega.Equal(esSrcIP), "source IP shouldn't be external service's source IP")

			// access from pod to remote server, directly
			daccessIP, _, err := execInPod(f.KubeClient, f.KubeConfig, ns, "pod1", "centos", []string{"curl", fmt.Sprintf("%s:%d", rsrvAddr, rsrvPort)})
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "error executing command in pod")
			// expect podIP or nodeIP (just check that it isn't podIP and externalservice's source IP)
			gomega.Expect(daccessIP).NotTo(gomega.Equal(esSrcIP), "source IP shouldn't be external service's source IP")

			// access from pod to remote server via external service
			esAccessIP, _, err := execInPod(f.KubeClient, f.KubeConfig, ns, "pod1", "centos", []string{"curl", fmt.Sprintf("es1.external-services:%d", esPort)})
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "error executing command in pod")
			gomega.Expect(esAccessIP).To(gomega.Equal(esSrcIP), "source IP should be external services' sourceIP")
		})
	})
})

func cleanupOptions(ctx *framework.TestCtx) *framework.CleanupOptions {
	return &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval}
}

func createExternalService(t *testing.T, f *framework.Framework, ctx *framework.TestCtx, ns string, es *v1alpha1.ExternalService, checkResource bool) {
	var err error

	// Create externalService
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
