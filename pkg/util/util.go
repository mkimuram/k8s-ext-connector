package util

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	yaml "gopkg.in/yaml.v2"
)

const (
	// nsPrefix is a prefix for a network namespace used for gateway
	nsPrefix = "ns"
	// vlanDevPrefix is a prefix for a vlan device used for gateway
	vlanDevPrefix = "macvlan"
	// pidFilePath is a path for pid file
	pidFilePath = "/run/"
	// sshdPidFilePrefix is a preffix for pid file of sshd
	sshdPidFilePrefix = "sshd-"
	// pidFileSuffix is a suffix for pid file
	pidFileSuffix = ".pid"
	// gatewayRulePrefix is a prefix for gateway rule configmap name
	gatewayRulePrefix = "gwrule"
	// minPort is the smallest port number that can be used by forwarder pod
	minPort = 2049
	// maxPort is the biggest port number that can be used by forwarder pod
	maxPort = 65536
)

// GetHexIP returns hex expression of IP address
// ex) 192.168.122.1 -> c0a87a01
func GetHexIP(ip string) (string, error) {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return "", fmt.Errorf("getHexIP: failed to parse ip %q", ip)
	}
	v4IP := parsedIP.To4()
	if v4IP == nil {
		return "", fmt.Errorf("getHexIP: failed to convert ip %v to 4 bytes", parsedIP)
	}

	return fmt.Sprintf("%02x%02x%02x%02x", v4IP[0], v4IP[1], v4IP[2], v4IP[3]), nil
}

// GetDecIPFromHexIP returns decimal expression of IP address
// ex) c0a87a01 -> 192.168.122.1
func GetDecIPFromHexIP(hexIP string) (string, error) {
	a, err := hex.DecodeString(hexIP)
	if err != nil {
		return "", err
	}
	if len(a) != 4 {
		return "", fmt.Errorf("%s isn't a valid IP", hexIP)
	}

	return fmt.Sprintf("%v.%v.%v.%v", a[0], a[1], a[2], a[3]), nil
}

// GetRuleName returns configmap name for gateway which has ip
// ex) 192.168.122.1 -> gwrulec0a87a01
func GetRuleName(ip string) (string, error) {
	hexIP, err := GetHexIP(ip)
	if err != nil {
		return "", err
	}

	return gatewayRulePrefix + hexIP, nil
}

// GetIPfromRuleName returns IP for configmap
// ex) gwrulec0a87a01 -> 192.168.122.1
func GetIPfromRuleName(ruleName string) (string, error) {
	return GetDecIPFromHexIP(strings.TrimPrefix(ruleName, gatewayRulePrefix))
}

// IsGatewayRule returns true if name is gatewayRule, otherwise return false
func IsGatewayRule(name string) bool {
	return strings.HasPrefix(name, gatewayRulePrefix)
}

// GetNs returns network namespace name for gateway which has ip
// ex) 192.168.122.1 -> nsc0a87a01
func GetNs(ip string) (string, error) {
	hexIP, err := GetHexIP(ip)
	if err != nil {
		return "", err
	}

	return nsPrefix + hexIP, nil
}

// GetVlanDev returns vlandevice name for gateway which has ip
// ex) 192.168.122.1 -> macvlanc0a87a01
func GetVlanDev(ip string) (string, error) {
	hexIP, err := GetHexIP(ip)
	if err != nil {
		return "", err
	}

	return vlanDevPrefix + hexIP, nil
}

// GetSshdPidFile returns name of pid file for sshd running in namespace which has ip
// ex) 192.168.122.1 -> sshd-c0a87ac8.pid
func GetSshdPidFile(ip string) (string, error) {
	return GetPidFile(ip, sshdPidFilePrefix)
}

// GetPidFile returns name of pid file in namespace which has ip
// ex) 192.168.122.1 -> {prefix}c0a87ac8.pid
func GetPidFile(ip, prefix string) (string, error) {
	hexIP, err := GetHexIP(ip)
	if err != nil {
		return "", err
	}
	pidFileName := prefix + hexIP + pidFileSuffix

	return filepath.Join(pidFilePath, pidFileName), nil
}

// GetIPs returns string array of IPs defined in configmap named {name} in namespace {namespace}
// Expected format of configmap is:
//  ips: |
//    192.168.122.200
//    192.168.122.201
// For above example, it returns ["192.168.122.200" "192.168.122.201"]
func GetIPs(clientset *kubernetes.Clientset, namespace, name string) ([]string, error) {
	cm, err := clientset.CoreV1().ConfigMaps(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return []string{}, fmt.Errorf("getIPs: configmap %q in %q namespace not found: %v", name, namespace, err)
		}
		return []string{}, err
	}

	if ips, ok := cm.Data["ips"]; ok {
		return strings.Fields(ips), nil
	}

	return []string{}, fmt.Errorf("getIPs: configmap %q in %q namespace not contains ips data", name, namespace)
}

// GetIptablesRules returns string array of iptables rules defined in configmap named {name} in namespace {namespace}
// Expected format of configmap is:
//  rules: |
//    PREROUTING -t nat -m tcp -p tcp --dst 192.168.122.200 --src 192.168.122.140 --dport 80 -j DNAT --to-destination 192.168.122.200:2049
//    POSTROUTING -t nat -m tcp -p tcp --dst 192.168.122.140 --dport 2049 -j SNAT --to-source 192.168.122.200
// For above example, it returns ["PREROUTING..." "POSTROUTING..."]
func GetIptablesRules(clientset *kubernetes.Clientset, namespace, name string) ([]string, error) {
	cm, err := clientset.CoreV1().ConfigMaps(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return []string{}, fmt.Errorf("getIptablesRules: configmap %q in %q namespace not found: %v", name, namespace, err)
		}
		return []string{}, err
	}

	if rules, ok := cm.Data["rules"]; ok {
		return strings.Split(rules, "\n"), nil
	}

	return []string{}, fmt.Errorf("getIptablesRules: configmap %q in %q namespace not contains rules data", name, namespace)
}

// GenPort returns string expression of port number that is not marked used in usedPorts
// It assigns unused port and updates the mapping in usedPorts
func GenPort(sourceIP string, targetPort string, usedPorts map[string]string) string {
	for port := minPort; port < maxPort+1; port++ {
		strPort := strconv.Itoa(port)
		if _, ok := usedPorts[strPort]; !ok {
			usedPorts[strPort] = sourceIP + ":" + targetPort
			return strPort
		}
	}

	return ""
}

// GetPort returns string expression of port number that is marked used for sourceIP and targetPort
// in usedPorts
func GetPort(sourceIP string, targetPort string, usedPorts map[string]string) string {
	for port, usedBy := range usedPorts {
		if usedBy == sourceIP+":"+targetPort {
			return port
		}
	}

	return ""
}

// GenRemotePort returns string expression of port number that is not marked used in usedRemotePorts
// It returns the same port as defined in usedRemotePorts if it is already assigned.
// If not, it assigns unused port and updates the mapping in usedRemotePorts
func GenRemotePort(port string, usedRemotePorts map[string]string) string {
	if val, ok := usedRemotePorts[port]; ok {
		return val
	}
	for fwdPort := minPort; fwdPort < maxPort+1; fwdPort++ {
		strFwdPort := strconv.Itoa(fwdPort)
		if _, ok := usedRemotePorts[strFwdPort]; !ok {
			// Reference for port to forward port
			usedRemotePorts[port] = strFwdPort
			// Reference for forward port to port
			usedRemotePorts[strFwdPort] = port
			return strFwdPort
		}
	}

	return ""
}

// GetRemoteFwdPort returns string expression of port number that is defined used in configmap
// Expected format of configmap is:
//  data.yaml: |
//    forwarder:
//      my-externalservice:
//        remote-ssh-tunnel: |
//          192.168.122.200:2049:10.96.223.183:80,192.168.122.200
//          192.168.122.201:2050:10.96.218.78:80,192.168.122.201
// For above example, it returns "2049", if clusterIP is "10.96.223.183",
// sourceIP is "192.168.122.200", and remotePort is "80"
func GetRemoteFwdPort(esconfig *corev1.ConfigMap, esName, clusterIP, sourceIP, remotePort string) (string, error) {
	var remoteRules string
	var hasData bool

	if data, ok := esconfig.Data["data.yaml"]; ok {
		yamlData := make(map[string]map[string]map[string]string)
		err := yaml.Unmarshal([]byte(data), yamlData)
		if err != nil {
			return "", err
		}
		if forwarder, ok := yamlData["forwarder"]; ok {
			if rules, ok := forwarder[esName]; ok {
				remoteRules, hasData = rules["remote-ssh-tunnel"]
			}
		}
	}
	if !hasData {
		return "", fmt.Errorf("getRemoteFwdPort: configMap doesn't have data")
	}
	for _, s := range strings.Split(string(remoteRules), "\n") {
		// Fields are like below:
		// {sourceIP}:{remoteFwdPort}:{clusterIP}:{remotePort},{sourceIP}
		// ex)
		// 192.168.122.200:2049:10.96.223.183:80,192.168.122.200
		commas := strings.Split(s, ",")
		if len(commas) < 2 {
			continue
		}
		cols := strings.Split(commas[0], ":")
		if len(cols) < 4 {
			continue
		}
		if cols[0] == sourceIP && cols[2] == clusterIP && cols[3] == remotePort {
			return cols[1], nil
		}
	}

	return "", fmt.Errorf("getRemoteFwdPort: remoteFwdPort not found")
}

// GetUsedRemotePorts returns string-to-string map of port number that is defined used in configmap
// Expected format of configmap is:
//  rules: |
//    PREROUTING -t nat -m tcp -p tcp --dst 192.168.122.200 --src 192.168.122.140 --dport 80 -j DNAT --to-destination 192.168.122.200:2049
//    POSTROUTING -t nat -m tcp -p tcp --dst 192.168.122.140 --dport 2049 -j SNAT --to-source 192.168.122.200
// For above example, it returns map[string]string{"80": "2049", "2049": "80"}
func GetUsedRemotePorts(client client.Client, namespace, sourceIP string) (map[string]string, error) {
	usedRemotePorts := map[string]string{}

	configmapName, err := GetRuleName(sourceIP)
	if err != nil {
		return usedRemotePorts, err
	}

	configMap := &corev1.ConfigMap{}
	if err := client.Get(context.TODO(), types.NamespacedName{Name: configmapName, Namespace: namespace}, configMap); err != nil && !errors.IsNotFound(err) {
		return usedRemotePorts, err
	}

	if rules, ok := configMap.Data["rules"]; ok {
		for _, s := range strings.Split(string(rules), "\n") {
			fields := strings.Fields(s)
			if len(fields) > 16 && fields[0] == "PREROUTING" {
				ipPort := strings.Split(fields[16], ":")
				if len(ipPort) > 1 {
					// Reference for port to forward port
					usedRemotePorts[fields[13]] = ipPort[1]
					// Reference for forward port to port
					usedRemotePorts[ipPort[1]] = fields[13]
				}
			}
		}
	}

	return usedRemotePorts, nil
}
