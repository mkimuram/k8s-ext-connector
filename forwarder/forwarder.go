package main

import (
	"flag"
	"os"

	"github.com/golang/glog"
	clversioned "github.com/mkimuram/k8s-ext-connector/pkg/client/clientset/versioned"
	clv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/client/clientset/versioned/typed/submariner/v1alpha1"
	"github.com/mkimuram/k8s-ext-connector/pkg/forwarder"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
)

var (
	namespace string
	name      string
	fwd       *forwarder.ForwarderController
)

func init() {
	flag.Set("logtostderr", "true")
	flag.Set("stderrthreshold", "INFO")
	flag.Parse()
	namespace = os.Getenv("FORWARDER_NAMESPACE")
	name = os.Getenv("FORWARDER_NAME")

	if namespace == "" || name == "" {
		glog.Fatalf("FORWARDER_NAMESPACE and FORWARDER_NAME need to be defined as environment variables")
	}

	// create in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		glog.Fatalf("Failed to build config: %v", err)
	}
	// create clientset
	cl, err := clv1alpha1.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Failed to create client: %v", err)
	}

	// create versioned clientset
	vcl, err := clversioned.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Failed to create versioned client: %v", err)
	}

	fwd = forwarder.NewForwarderController(cl, vcl, namespace, name)
}

func main() {
	fwd.Run()
}
