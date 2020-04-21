package main

import (
	"flag"
	"os"

	"github.com/golang/glog"
	clv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/client/clientset/versioned/typed/submariner/v1alpha1"
	"github.com/mkimuram/k8s-ext-connector/pkg/forwarder"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
)

var (
	namespace string
	name      string
	fwd       *forwarder.Forwarder
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

	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	// creates the clientset
	clientset, err := clv1alpha1.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	fwd = forwarder.NewForwarder(clientset, namespace, name)
}

func main() {
	fwd.Reconcile()
}
