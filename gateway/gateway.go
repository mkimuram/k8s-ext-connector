package main

import (
	"flag"
	"os"
	"path/filepath"

	"github.com/golang/glog"
	"github.com/mkimuram/k8s-ext-connector/pkg/gateway"

	clversioned "github.com/mkimuram/k8s-ext-connector/pkg/client/clientset/versioned"
	clv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/client/clientset/versioned/typed/submariner/v1alpha1"
	"k8s.io/client-go/tools/clientcmd"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var (
	kubeconfig *string
	cl         *clv1alpha1.SubmarinerV1alpha1Client
	vcl        *clversioned.Clientset
	namespace  = flag.String("namespace", "external-services", "Kubernetes's namespace to watch for.")
	g          *gateway.GatewayController
)

func init() {
	//var kubeconfig *string
	if home := os.Getenv("HOME"); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		glog.Fatalf("Failed to build config from %q: %v", *kubeconfig, err)
	}

	// create clientset
	cl, err = clv1alpha1.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Failed to create client from %q: %v", *kubeconfig, err)
	}
	// create versioned clientset
	vcl, err = clversioned.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Failed to create versioned client from %q: %v", *kubeconfig, err)
	}

	g = gateway.NewGatewayController(cl, vcl, *namespace)
}

func main() {
	g.Run()
}
