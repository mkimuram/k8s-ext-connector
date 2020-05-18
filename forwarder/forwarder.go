package main

import (
	"flag"
	"os"
	"time"

	"github.com/golang/glog"
	clversioned "github.com/mkimuram/k8s-ext-connector/pkg/client/clientset/versioned"
	clv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/client/clientset/versioned/typed/submariner/v1alpha1"
	sbinformers "github.com/mkimuram/k8s-ext-connector/pkg/client/informers/externalversions"
	"github.com/mkimuram/k8s-ext-connector/pkg/forwarder"
	"github.com/mkimuram/k8s-ext-connector/pkg/util"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
)

var (
	namespace string
	name      string
	fwd       *util.Controller
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

	informerFactory := sbinformers.NewSharedInformerFactory(vcl, time.Second*30)
	informer := informerFactory.Submariner().V1alpha1().Forwarders().Informer()
	reconciler := forwarder.NewReconciler(cl, namespace, name)
	fwd = util.NewController(cl, informerFactory, informer, reconciler)
}

func main() {
	fwd.Run()
}
