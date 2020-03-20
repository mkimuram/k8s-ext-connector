package main

import (
	"flag"
	"os"
	"path/filepath"

	"github.com/golang/glog"
	"github.com/mkimuram/k8s-ext-connector/pkg/gateway"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var (
	kubeconfig      *string
	clientset       *kubernetes.Clientset
	nic             = flag.String("nic", "eth0", "Name of the nic for parent device of the macvlan device.")
	netmask         = flag.String("netmask", "24", "Netmask for the gateway in numerical format.")
	defaultGW       = flag.String("defaultGW", "192.168.122.1", "Default gateway for the device.")
	configNamespace = flag.String("configNamespace", "external-services", "Kubernetes's namespace that configmap exists.")
	ipConfigName    = flag.String("configName", "ips", "Name of the configmap that contains list of IPs.")

	gw *gateway.Gateway
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
		glog.Errorf("Failed to build config from %q: %v", *kubeconfig, err)
		os.Exit(1)
	}

	// create the clientset
	clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		glog.Errorf("Failed to create client from %q: %v", *kubeconfig, err)
		os.Exit(1)
	}

	gw = gateway.NewGateway(clientset, *nic, *netmask, *defaultGW, *configNamespace, *ipConfigName)
}

func main() {
	gw.Reconcile()
}
