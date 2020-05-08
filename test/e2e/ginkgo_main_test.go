package e2e

import (
	"fmt"
	"testing"
	"time"

	"github.com/mkimuram/k8s-ext-connector/pkg/apis"
	"github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/operator-framework/operator-sdk/pkg/test/e2eutil"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	operatorName = "k8s-ext-connector"
)

var (
	retryInterval        = time.Second * 5
	timeout              = time.Second * 60
	cleanupRetryInterval = time.Second * 1
	cleanupTimeout       = time.Second * 5

	objs = []runtime.Object{
		&v1alpha1.ExternalServiceList{},
		&v1alpha1.ForwarderList{},
		&v1alpha1.GatewayList{},
	}
	Testing *testing.T
)

func TestE2e(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	Testing = t
	ginkgo.RunSpecs(t, "E2e Suite")
}

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
