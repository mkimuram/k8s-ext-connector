package e2e

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/mkimuram/k8s-ext-connector/pkg/apis"
	"github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/operator-framework/operator-sdk/pkg/test/e2eutil"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

const (
	operatorName = "k8s-ext-connector"
)

var (
	retryInterval        = time.Second * 5
	timeout              = time.Second * 60
	cleanupRetryInterval = time.Second * 1
	cleanupTimeout       = time.Second * 60

	objs = []runtime.Object{
		&v1alpha1.ExternalServiceList{},
		&v1alpha1.ForwarderList{},
		&v1alpha1.GatewayList{},
	}
	Testing *testing.T

	ipstr string
	ips   []string
)

func init() {
	flag.StringVar(&ipstr, "ips", "", "comma separated list of IPs to be used for tests")
}

func TestE2e(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	Testing = t
	ginkgo.RunSpecs(t, "E2e Suite")
}

var _ = ginkgo.SynchronizedBeforeSuite(
	// Run only on Ginkgo node 1
	func() []byte {
		return nil
	},
	// Run on all Ginkgo nodes
	func(data []byte) {
		ips = strings.Split(ipstr, ",")
	},
)

func initOperator(t *testing.T, name string, objs []runtime.Object) (*framework.TestCtx, string) {
	for _, obj := range objs {
		err := framework.AddToFrameworkScheme(apis.AddToScheme, obj)
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "failed to add custom resource scheme to framework")
	}

	ctx := framework.NewTestCtx(t)
	err := ctx.InitializeClusterResources(&framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "failed to initialize cluster resources")

	fmt.Fprintf(ginkgo.GinkgoWriter, "Initialized cluster resources\n")

	namespace, err := ctx.GetNamespace()
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "failed to get namespace")

	// get global framework variables
	f := framework.Global
	// wait for memcached-operator to be ready
	err = e2eutil.WaitForOperatorDeployment(t, f.KubeClient, namespace, name, 1, retryInterval, timeout)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "operator failed to be ready")

	return ctx, namespace
}

func runGateway(ctx context.Context) {
	go func() {
		defer ginkgo.GinkgoRecover()

		cmd := exec.CommandContext(ctx, "gateway/bin/gateway")
		err := cmd.Start()
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "failed to start gateway")

		err = cmd.Wait()
	}()
}

type handler struct{}

func (h handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// return only IP fields of remote address (= Source IP)
	s := strings.SplitN(r.RemoteAddr, ":", 2)
	w.Write([]byte(s[0]))
}

func runRemoteServer(ctx context.Context, addr, port string) {
	srv := http.Server{Addr: addr + ":" + port, Handler: &handler{}}
	go func() {
		select {
		case <-ctx.Done():
			srv.Shutdown(context.Background())
		default:
			defer ginkgo.GinkgoRecover()
			err := srv.ListenAndServe()
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "failed to start remote server")
		}
	}()
}

func execInPod(cl kubernetes.Interface, config *rest.Config, namespace, podName, containerName string, command []string) (string, string, error) {
	req := cl.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   command,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, clientscheme.ParameterCodec)

	rexec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return "", "", err
	}

	var stdout, stderr bytes.Buffer
	err = rexec.Stream(remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &stdout,
		Stderr: &stderr,
		Tty:    false,
	})

	return stdout.String(), stderr.String(), err
}
