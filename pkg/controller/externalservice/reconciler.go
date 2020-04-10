package externalservice

import (
	"context"
	"fmt"
	"strconv"

	"github.com/go-logr/logr"
	submarinerv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	"github.com/mkimuram/k8s-ext-connector/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	yaml "gopkg.in/yaml.v2"
)

// blank assignment to verify that ReconcileExternalService implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileExternalService{}

// ReconcileExternalService reconciles a ExternalService object
type ReconcileExternalService struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a ExternalService object and makes changes based on the state read
// and what is in the ExternalService.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileExternalService) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling ExternalService")

	// Fetch the ExternalService instance
	instance := &submarinerv1alpha1.ExternalService{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// ExternalService CR is deleted, so clean up the related resources, then clear finalizer
	if instance.GetDeletionTimestamp() != nil {
		// Clean up related resources
		if err := r.deleteResourceForExternalService(instance); err != nil {
			return reconcile.Result{}, err
		}

		// Clear finalizer
		instance.SetFinalizers(nil)
		if err := r.client.Update(context.TODO(), instance); err != nil {
			return reconcile.Result{}, err
		}

		return reconcile.Result{}, nil
	}

	// Add finalizer for this CR
	if err := r.addFinalizer(reqLogger, instance); err != nil {
		return reconcile.Result{}, err
	}

	// Define a new forwarder Pod object
	pod := genForwardPodSpec(instance)

	// Check if this Pod already exists
	foundPod := &corev1.Pod{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, foundPod)
	if err != nil && !errors.IsNotFound(err) {
		return reconcile.Result{}, err
	} else if err != nil {
		// Ensure configmap exists before creating forwarder pod
		_, err := util.GetOrCreateConfigMap(r.client, instance.Name, ConnectorNamespace)
		if err != nil {
			return reconcile.Result{}, err
		}

		reqLogger.Info("Creating a new Pod", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)
		err = r.client.Create(context.TODO(), pod)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	// Define a new forwarder service object
	service := genForwardServiceSpec(instance)

	// Check if this service already exists
	foundSvc := &corev1.Service{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, foundSvc)
	if err != nil && !errors.IsNotFound(err) {
		return reconcile.Result{}, err
	} else if err != nil {
		reqLogger.Info("Creating a new service", "Service.Namespace", service.Namespace, "Serivce.Name", service.Name)
		err = r.client.Create(context.TODO(), service)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	// Update configmap for forwarder pod
	// TODO: Remove this after crd version is implemented
	err = r.updateConfigmapDataForForwarder(instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Update configmap for gateways
	// TODO: Remove this after crd version is implemented
	err = r.updateConfigmapDataForGateways(instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Get list of all forwarders
	fwds := &submarinerv1alpha1.ForwarderList{}
	opts := []client.ListOption{}
	if err := r.client.List(context.TODO(), fwds, opts...); err != nil {
		return reconcile.Result{}, err
	}

	// Get list of all gateways
	gws := &submarinerv1alpha1.GatewayList{}
	if err := r.client.List(context.TODO(), gws, opts...); err != nil {
		return reconcile.Result{}, err
	}

	// Update forwarder CRD
	err = updateForwarderRules(r.client, instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Update Gateway CRD
	err = updateGatewayRules(r.client, instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func genUsedPortsForEgress(fwd *submarinerv1alpha1.Forwarder) map[string]string {
	usedPorts := map[string]string{}
	for _, erule := range fwd.Spec.EgressRules {
		usedPorts[erule.RelayPort] = erule.TargetPort
	}
	return usedPorts
}

func genRelayPortForEgress(srcIP string, tPort string, ePorts map[string]string) (string, error) {
	// Find relayPort from ePorts and return the value if already exists
	for k, v := range ePorts {
		if v == srcIP+":"+tPort {
			return k, nil
		}
	}

	// Assign new relayPort and update ePorts
	for port := util.MinPort; port < util.MaxPort+1; port++ {
		strPort := strconv.Itoa(port)
		if _, ok := ePorts[strPort]; !ok {
			ePorts[strPort] = srcIP + ":" + tPort
			return strPort, nil
		}
	}

	return "", fmt.Errorf("RelayPort exhausted")
}

func genUsedPortsForIngress(gws *submarinerv1alpha1.GatewayList) map[string]map[string]string {
	usedPorts := map[string]map[string]string{}
	for _, gw := range gws.Items {
		if _, ok := usedPorts[gw.Name]; !ok {
			usedPorts[gw.Name] = map[string]string{}
		}
		for _, irule := range gw.Spec.IngressRules {
			usedPorts[gw.Name][irule.RelayPort] = irule.SourceIP + ":" + irule.TargetPort
		}
	}
	return usedPorts
}

func genRelayPortForIngress(srcIP string, tPort string, gwName string, iPorts map[string]map[string]string) (string, error) {
	if _, ok := iPorts[gwName]; !ok {
		iPorts[gwName] = map[string]string{}
	}
	// Find relayPort from ePorts and return the value if already exists
	for k, v := range iPorts[gwName] {
		if v == srcIP+":"+tPort {
			return k, nil
		}
	}

	// Assign new relayPort and update ePorts
	for port := util.MinPort; port < util.MaxPort+1; port++ {
		strPort := strconv.Itoa(port)
		if _, ok := iPorts[gwName][strPort]; !ok {
			iPorts[gwName][strPort] = srcIP + ":" + tPort
			return strPort, nil
		}
	}

	return "", fmt.Errorf("RelayPort exhausted")
}

func getEndpointAddrs(cl client.Client, ns string, name string) ([]string, error) {
	addrs := []string{}

	ep := &corev1.Endpoints{}
	err := cl.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: ns}, ep)
	if err != nil && !errors.IsNotFound(err) {
		return addrs, err
	}
	for _, subset := range ep.Subsets {
		for _, addr := range subset.Addresses {
			addrs = append(addrs, addr.IP)
		}
	}

	return addrs, nil
}

func genForwarderEgressRules(cl client.Client, cr *submarinerv1alpha1.ExternalService, ePorts map[string]string) ([]submarinerv1alpha1.ForwarderRule, error) {
	eRules := []submarinerv1alpha1.ForwarderRule{}

	for _, src := range cr.Spec.Sources {
		// Create gateway ref from SourceIP
		gwName, err := util.GetRuleName(src.SourceIP)
		if err != nil {
			return eRules, err
		}
		gw := submarinerv1alpha1.GatewayRef{
			Namespace: ConnectorNamespace,
			Name:      gwName,
		}

		// Get endpoint addresses for src
		addrs, err := getEndpointAddrs(cl, src.Service.Namespace, src.Service.Name)
		if err != nil {
			return eRules, err
		}

		for _, port := range cr.Spec.Ports {
			for _, srcIP := range addrs {
				rPort, err := genRelayPortForEgress(srcIP, port.TargetPort.String(), ePorts)
				if err != nil {
					return eRules, err
				}
				er := submarinerv1alpha1.ForwarderRule{
					Protocol:        string(port.Protocol),
					SourceIP:        srcIP,
					TargetPort:      port.TargetPort.String(),
					DestinationPort: strconv.Itoa(int(port.Port)),
					DestinationIP:   cr.Spec.TargetIP,
					Gateway:         gw,
					GatewayIP:       src.SourceIP,
					RelayPort:       rPort,
				}
				eRules = append(eRules, er)
			}
		}
	}

	return eRules, nil
}

func genForwarderIngressRules(cl client.Client, cr *submarinerv1alpha1.ExternalService, iPorts map[string]map[string]string) ([]submarinerv1alpha1.ForwarderRule, error) {
	reqLogger := log.WithValues("Request.Namespace", cr.Namespace, "Request.Name", cr.Name)
	reqLogger.Info("genForwarderIngressRules")
	iRules := []submarinerv1alpha1.ForwarderRule{}

	for _, src := range cr.Spec.Sources {
		// Create gateway ref from SourceIP
		gwName, err := util.GetRuleName(src.SourceIP)
		if err != nil {
			return iRules, err
		}
		gw := submarinerv1alpha1.GatewayRef{
			Namespace: ConnectorNamespace,
			Name:      gwName,
		}

		svc := &corev1.Service{}
		err = cl.Get(context.TODO(), types.NamespacedName{Name: src.Service.Name, Namespace: src.Service.Namespace}, svc)
		if err != nil && !errors.IsNotFound(err) {
			return iRules, err
		}

		for _, svcPort := range svc.Spec.Ports {
			rPort, err := genRelayPortForIngress(cr.Spec.TargetIP, strconv.Itoa(int(svcPort.Port)), gwName, iPorts)
			reqLogger.Info("genRelayPortForIngress", "targetIP", cr.Spec.TargetIP, "port", strconv.Itoa(int(svcPort.Port)), "gwName", gwName, "rPort", rPort, "iPorts", iPorts)
			if err != nil {
				return iRules, err
			}

			ir := submarinerv1alpha1.ForwarderRule{
				Protocol:        string(svcPort.Protocol),
				SourceIP:        cr.Spec.TargetIP,
				TargetPort:      strconv.Itoa(int(svcPort.Port)),
				DestinationPort: strconv.Itoa(int(svcPort.Port)),
				DestinationIP:   svc.Spec.ClusterIP,
				Gateway:         gw,
				GatewayIP:       src.SourceIP,
				RelayPort:       rPort,
			}
			iRules = append(iRules, ir)
		}
	}

	return iRules, nil
}

func updateForwarderRules(cl client.Client, cr *submarinerv1alpha1.ExternalService) error {
	reqLogger := log.WithValues("Request.Namespace", cr.Namespace, "Request.Name", cr.Name)
	reqLogger.Info("updateForwarderRules")

	fwd := &submarinerv1alpha1.Forwarder{}
	// TODO: specify the right name
	err := cl.Get(context.TODO(), types.NamespacedName{Name: cr.Name, Namespace: ConnectorNamespace}, fwd)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create a new empty forwarder CRD
			fwd = &submarinerv1alpha1.Forwarder{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cr.Name,
					Namespace: ConnectorNamespace,
				},
			}
			if err := cl.Create(context.TODO(), fwd); err != nil {
				return err
			}
		} else {
			return err
		}
	}
	// Update RuleUpdatingCondition to true
	if fwd.Status.Conditions.SetCondition(util.RuleUpdatingCondition(corev1.ConditionTrue)) {
		if err := cl.Status().Update(context.TODO(), fwd); err != nil {
			return err
		}
		reqLogger.Info("Update RuleUpdatingCondition to true", "forwarder", fwd.Name)
	}

	// Get forwarderPod's IP
	fwdPod := &corev1.Pod{}
	err = cl.Get(context.TODO(), types.NamespacedName{Name: cr.Name, Namespace: ConnectorNamespace}, fwdPod)
	if err != nil {
		return err
	}

	// Generate new rules
	ePorts := genUsedPortsForEgress(fwd)
	eRules, err := genForwarderEgressRules(cl, cr, ePorts)
	if err != nil {
		return err
	}

	// Get list of all gateways
	gws := &submarinerv1alpha1.GatewayList{}
	opts := []client.ListOption{}
	if err := cl.List(context.TODO(), gws, opts...); err != nil {
		return err
	}
	iPorts := genUsedPortsForIngress(gws)
	iRules, err := genForwarderIngressRules(cl, cr, iPorts)
	if err != nil {
		return err
	}

	// Update with new rule
	fwd.Spec.EgressRules = eRules
	fwd.Spec.IngressRules = iRules
	fwd.Spec.ForwarderIP = fwdPod.Status.PodIP
	// TODO: skip updating if there are no changes
	if err := cl.Update(context.TODO(), fwd); err != nil {
		return err
	}
	reqLogger.Info("Update to new rule", "forwarder", fwd.Name, "egressRules", eRules, "ingressRules", iRules)

	// Update RuleUpdatingCondition to false
	updateChanged := fwd.Status.Conditions.SetCondition(util.RuleUpdatingCondition(corev1.ConditionFalse))
	syncChanged := fwd.Status.Conditions.SetCondition(util.RuleSyncingCondition(corev1.ConditionUnknown))
	if updateChanged || syncChanged {
		fwd.Status.RuleGeneration += 1
		if err := cl.Status().Update(context.TODO(), fwd); err != nil {
			return err
		}
		reqLogger.Info("Update RuleUpdatingCondition to false and SyncingCondition to unknown", "forwarder", fwd.Name)
	}

	return nil
}

func genGatewayEgressRules(cl client.Client, fwds *submarinerv1alpha1.ForwarderList, gw *submarinerv1alpha1.Gateway) []submarinerv1alpha1.GatewayRule {
	reqLogger := log.WithValues("gw.Namespace", gw.Namespace, "gw.Name", gw.Name)
	reqLogger.Info("genGatewayEgressRules")

	egressRules := []submarinerv1alpha1.GatewayRule{}

	for _, fwd := range fwds.Items {
		for _, rule := range fwd.Spec.EgressRules {
			if rule.Gateway.Namespace != gw.Namespace || rule.Gateway.Name != gw.Name {
				// Skip unrelated forwarder
				continue
			}
			reqLogger.Info("values", "SourceIP", rule.SourceIP, "TargetPort", rule.TargetPort, "RelayPort", rule.RelayPort)
			eRule := submarinerv1alpha1.GatewayRule{
				Protocol:        rule.Protocol,
				SourceIP:        rule.SourceIP,
				TargetPort:      rule.TargetPort,
				DestinationIP:   rule.DestinationIP,
				DestinationPort: rule.DestinationPort,
				Forwarder: submarinerv1alpha1.ForwarderRef{
					Namespace: fwd.Namespace,
					Name:      fwd.Name,
				},
				ForwarderIP: fwd.Spec.ForwarderIP,
				RelayPort:   rule.RelayPort,
			}
			egressRules = append(egressRules, eRule)
		}
	}

	return egressRules
}

func genGatewayIngressRules(cl client.Client, fwds *submarinerv1alpha1.ForwarderList, gw *submarinerv1alpha1.Gateway) []submarinerv1alpha1.GatewayRule {
	ingressRules := []submarinerv1alpha1.GatewayRule{}

	for _, fwd := range fwds.Items {
		for _, rule := range fwd.Spec.IngressRules {
			if rule.Gateway.Namespace != gw.Namespace || rule.Gateway.Name != gw.Name {
				// Skip unrelated forwarder
				continue
			}
			iRule := submarinerv1alpha1.GatewayRule{
				Protocol:        rule.Protocol,
				SourceIP:        rule.SourceIP,
				TargetPort:      rule.TargetPort,
				DestinationIP:   rule.DestinationIP,
				DestinationPort: rule.DestinationPort,
				Forwarder: submarinerv1alpha1.ForwarderRef{
					Namespace: fwd.Namespace,
					Name:      fwd.Name,
				},
				ForwarderIP: fwd.Spec.ForwarderIP,
				RelayPort:   rule.RelayPort,
			}
			ingressRules = append(ingressRules, iRule)
		}
	}

	return ingressRules
}

func updateRulesForOneGateway(cl client.Client, fwds *submarinerv1alpha1.ForwarderList, gw *submarinerv1alpha1.Gateway, gwIP string) error {
	reqLogger := log.WithValues("Gateway.Namespace", gw.Namespace, "Gateway.Name", gw.Name)
	reqLogger.Info("updateRulesForOneGateway")
	// Update RuleUpdatingCondition to true
	if gw.Status.Conditions.SetCondition(util.RuleUpdatingCondition(corev1.ConditionTrue)) {
		if err := cl.Status().Update(context.TODO(), gw); err != nil {
			return err
		}
		reqLogger.Info("Update RuleUpdatingCondition to true", "gateway", gw.Name)
	}

	// Generate new rules
	gw.Spec.EgressRules = genGatewayEgressRules(cl, fwds, gw)
	gw.Spec.IngressRules = genGatewayIngressRules(cl, fwds, gw)
	gw.Spec.GatewayIP = gwIP
	// Update with new rule
	// TODO: skip updating if there are no changes
	if err := cl.Update(context.TODO(), gw); err != nil {
		return err
	}
	reqLogger.Info("Update to new rule", "gateway", gw.Name, "egressRules", gw.Spec.EgressRules, "ingressRules", gw.Spec.IngressRules)

	// Update UpdatingCondition to false and SyncingCondition to unknown
	updateChanged := gw.Status.Conditions.SetCondition(util.RuleUpdatingCondition(corev1.ConditionFalse))
	syncChanged := gw.Status.Conditions.SetCondition(util.RuleSyncingCondition(corev1.ConditionUnknown))
	if updateChanged || syncChanged {
		gw.Status.RuleGeneration += 1
		if err := cl.Status().Update(context.TODO(), gw); err != nil {
			return err
		}
		reqLogger.Info("Update RuleUpdatingCondition to false and RuleSyncingCondition to unknown", "gateway", gw.Name)
	}

	return nil
}

func getUniqueGatwey(rules []submarinerv1alpha1.ForwarderRule) map[string]types.NamespacedName {
	nMap := map[string]types.NamespacedName{}

	// Make a map to remove duplicated
	for _, rule := range rules {
		val := rule.GatewayIP
		if _, ok := nMap[val]; ok {
			// skip adding to map
			continue
		}
		nMap[val] = types.NamespacedName{
			Namespace: rule.Gateway.Namespace,
			Name:      rule.Gateway.Name,
		}
	}

	return nMap
}

func updateGatewayRules(cl client.Client, cr *submarinerv1alpha1.ExternalService) error {
	reqLogger := log.WithValues("Request.Namespace", cr.Namespace, "Request.Name", cr.Name)
	reqLogger.Info("updateGatewayRules")

	// Get target forwarder to handle
	fwd := &submarinerv1alpha1.Forwarder{}
	// TODO: specify the right name
	err := cl.Get(context.TODO(), types.NamespacedName{Name: cr.Name, Namespace: ConnectorNamespace}, fwd)
	if err != nil {
		// Return error even for not found case and wait for forwarder to be created
		return err
	}

	// Get list of all forwarders
	fwds := &submarinerv1alpha1.ForwarderList{}
	opts := []client.ListOption{}
	if err := cl.List(context.TODO(), fwds, opts...); err != nil {
		return err
	}

	for gwIP, n := range getUniqueGatwey(fwd.Spec.IngressRules) {
		gw := &submarinerv1alpha1.Gateway{}
		err := cl.Get(context.TODO(), n, gw)
		if err != nil {
			if errors.IsNotFound(err) {
				// Create a new empty gateway CRD
				gw = &submarinerv1alpha1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: n.Namespace,
						Name:      n.Name,
					},
				}
				if err := cl.Create(context.TODO(), gw); err != nil {
					return err
				}
			} else {
				return err
			}
		}

		if err := updateRulesForOneGateway(cl, fwds, gw, gwIP); err != nil {
			return err
		}
	}

	return nil
}

// updateConfigmapDataForForwarder updates configmap data for the CR
func (r *ReconcileExternalService) updateConfigmapDataForForwarder(cr *submarinerv1alpha1.ExternalService) error {
	// Get or create configmap to update
	config, err := util.GetOrCreateConfigMap(r.client, cr.Name, ConnectorNamespace)
	if err != nil {
		return err
	}

	// Update data
	data := r.genForwarderRules(cr)
	configmapData := map[string]string{"data.yaml": data}

	// Update configmap with the data
	if err := util.UpdateConfigmapData(r.client, config, configmapData); err != nil {
		return err
	}

	return nil
}

func (r *ReconcileExternalService) updateConfigmapDataForGateways(cr *submarinerv1alpha1.ExternalService) error {
	// Get list of externalService
	// TODO: Use selector to get only necessary external service inside the loop.
	// Currently, only namespace and name is allowd for field selector (k/k#53459).
	// So, we need to add labels and use label selector, instead.
	eslist := &submarinerv1alpha1.ExternalServiceList{}
	opts := []client.ListOption{}
	if err := r.client.List(context.TODO(), eslist, opts...); err != nil {
		return err
	}

	// Update all configmap of the gateway for sources
	for _, source := range cr.Spec.Sources {
		rules, err := r.getGwRulesForSource(eslist, source)
		if err != nil {
			// TODO: handle error properly
			continue
		}

		configMapName, err := util.GetRuleName(source.SourceIP)
		if err != nil {
			// TODO: handle error properly
			continue
		}

		configMap, err := util.GetOrCreateConfigMap(r.client, configMapName, ConnectorNamespace)
		if err != nil {
			// TODO: handle error properly
			continue
		}

		// Update rules
		configmapData := map[string]string{"rules": rules}

		// Update configmap
		if err := util.UpdateConfigmapData(r.client, configMap, configmapData); err != nil {
			// TODO: handle error properly
			continue
		}
	}

	return nil
}

func (r *ReconcileExternalService) getGwRulesForSource(
	eslist *submarinerv1alpha1.ExternalServiceList,
	source submarinerv1alpha1.Source,
) (string, error) {

	rules := ""
	// Loop over all service in externalService's sources
	for _, es := range eslist.Items {
		for _, src := range es.Spec.Sources {
			// Update is only needed for the same sourceIP
			if src.SourceIP != source.SourceIP {
				continue
			}
			rs, err := r.getGwRule(src, es.Name, es.Spec.TargetIP)
			if err != nil {
				// TODO: handle error properly
				continue
			}
			rules += rs
		}
	}
	return rules, nil
}

func (r *ReconcileExternalService) getGwRule(src submarinerv1alpha1.Source, esName, targetIP string) (string, error) {
	rules := ""

	// Get service for src
	svc := &corev1.Service{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: src.Service.Name, Namespace: src.Service.Namespace}, svc)
	if err != nil && !errors.IsNotFound(err) {
		// TODO: handle error properly
		return "", fmt.Errorf("")
	}

	// Get configmap for esName
	esConfig := &corev1.ConfigMap{}
	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: esName, Namespace: ConnectorNamespace}, esConfig); err != nil && !errors.IsNotFound(err) {
		// TODO: handle error properly
		return "", fmt.Errorf("")
	}
	for _, rport := range svc.Spec.Ports {
		remotePort := strconv.Itoa(int(rport.Port))
		// Find remoteFwdPort from esConfig which has combination of clusterIP, sourceIP, and remotePort
		remoteFwdPort, err := util.GetRemoteFwdPort(esConfig, svc.Spec.ClusterIP, src.SourceIP, remotePort)
		if err != nil {
			// TODO: handle error properly
			continue
		}
		rules += fmt.Sprintf("PREROUTING -t nat -m tcp -p tcp --dst %s --src %s --dport %s -j DNAT --to-destination %s:%s\n",
			src.SourceIP, targetIP, remotePort, src.SourceIP, remoteFwdPort)
		rules += fmt.Sprintf("POSTROUTING -t nat -m tcp -p tcp --dst %s --dport %s -j SNAT --to-source %s\n",
			targetIP, remoteFwdPort, src.SourceIP)
	}

	return rules, nil
}

func (r *ReconcileExternalService) genForwarderRules(cr *submarinerv1alpha1.ExternalService) string {
	usedPorts := map[string]string{}
	sshTunnelRules := r.genSSHTunnelRules(cr, usedPorts)
	remoteSSHTunnelRules := r.genRemoteSSHTunnelRules(cr)
	iptablesRules := r.genIptablesRules(cr, usedPorts)

	data := map[string]string{"ssh-tunnel": sshTunnelRules, "remote-ssh-tunnel": remoteSSHTunnelRules, "iptables-rule": iptablesRules}

	byteData, err := yaml.Marshal(data)
	if err != nil {
		// TODO: Fix me
		return ""
	}
	return string(byteData[:])
}

func (r *ReconcileExternalService) genSSHTunnelRules(cr *submarinerv1alpha1.ExternalService, usedPorts map[string]string) string {
	rules := ""

	for _, source := range cr.Spec.Sources {
		for _, port := range cr.Spec.Ports {
			fwdPort := util.GenPort(source.SourceIP, port.TargetPort.String(), usedPorts)
			// Skip generating rules if any of values are not available
			if fwdPort == "" || cr.Spec.TargetIP == "" || port.TargetPort.String() == "" || source.SourceIP == "" {
				continue
			}
			rules += fmt.Sprintf("%s:%s:%s %s\n", fwdPort, cr.Spec.TargetIP, port.TargetPort.String(), source.SourceIP)
		}
	}

	return rules
}

func (r *ReconcileExternalService) genRemoteSSHTunnelRules(cr *submarinerv1alpha1.ExternalService) string {
	rules := ""

	for _, source := range cr.Spec.Sources {
		svc := &corev1.Service{}
		err := r.client.Get(context.TODO(), types.NamespacedName{Name: source.Service.Name, Namespace: source.Service.Namespace}, svc)
		if err != nil && !errors.IsNotFound(err) {
			// TODO: Handle error properly
			continue
		}
		clusterIP := svc.Spec.ClusterIP

		usedRemotePorts, err := util.GetUsedRemotePorts(r.client, ConnectorNamespace, source.SourceIP)
		if err != nil {
			// TODO: Handle error properly
			continue
		}

		for _, svcPort := range svc.Spec.Ports {
			remoteFwdPort := util.GenRemotePort(strconv.Itoa(int(svcPort.Port)), usedRemotePorts)
			// Skip generating rules if any of values are not available
			if source.SourceIP == "" || remoteFwdPort == "" || clusterIP == "" || strconv.Itoa(int(svcPort.Port)) == "" {
				continue
			}
			rules += fmt.Sprintf("%s:%s:%s:%s %s\n", source.SourceIP, remoteFwdPort, clusterIP, strconv.Itoa(int(svcPort.Port)), source.SourceIP)
		}
	}

	return rules
}

func (r *ReconcileExternalService) genIptablesRules(cr *submarinerv1alpha1.ExternalService, usedPorts map[string]string) string {
	logger := log.WithValues("Namespace", cr.Namespace, "Name", cr.Name)
	logger.Info("genIptablesRules")

	rules := ""

	fwdPod := &corev1.Pod{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: cr.Name, Namespace: ConnectorNamespace}, fwdPod)
	if err != nil && !errors.IsNotFound(err) {
		// TODO: Handle error properly
		return ""
	}
	fwdPodIP := fwdPod.Status.PodIP
	logger.Info("fwdPod", "fwdPodIP", fwdPodIP)

	for _, source := range cr.Spec.Sources {
		logger.Info("service", "name", source.Service.Name, "namespace", source.Service.Namespace)
		endpoint := &corev1.Endpoints{}
		err := r.client.Get(context.TODO(), types.NamespacedName{Name: source.Service.Name, Namespace: source.Service.Namespace}, endpoint)
		if err != nil && !errors.IsNotFound(err) {
			// TODO: Handle error properly
			logger.Info("getEndpoint", "err", err)
			continue
		}
		logger.Info("getEndpoint", "endpoint", endpoint)
		for _, port := range cr.Spec.Ports {
			fwdPort := util.GetPort(source.SourceIP, port.TargetPort.String(), usedPorts)
			logger.Info("port", "port", port.TargetPort.String(), "fwdPort", fwdPort)

			for _, subset := range endpoint.Subsets {
				for _, addr := range subset.Addresses {
					logger.Info("Values:", "fwdPodIP", fwdPodIP, "IP", addr.IP, "TargetPort", port.TargetPort.String(), "fwdPort", fwdPort, "TargetIP", cr.Spec.TargetIP)
					// Skip generating rules if any of values are not available
					if fwdPodIP == "" || addr.IP == "" || port.TargetPort.String() == "" || fwdPort == "" || cr.Spec.TargetIP == "" {
						continue
					}
					// TODO: Also handle UDP properly
					rules += fmt.Sprintf("PREROUTING -t nat -m tcp -p tcp --dst %s --src %s --dport %s -j DNAT --to-destination %s:%s\n",
						fwdPodIP, addr.IP, port.TargetPort.String(), fwdPodIP, fwdPort)
					rules += fmt.Sprintf("POSTROUTING -t nat -m tcp -p tcp --dst %s --dport %s -j SNAT --to-source %s\n",
						cr.Spec.TargetIP, fwdPort, fwdPodIP)
				}
			}
		}
	}

	return rules
}

func (r *ReconcileExternalService) addFinalizer(reqLogger logr.Logger, cr *submarinerv1alpha1.ExternalService) error {
	if len(cr.GetFinalizers()) < 1 && cr.GetDeletionTimestamp() == nil {
		reqLogger.Info("Adding Finalizer to ExternalService")

		cr.SetFinalizers([]string{ExternalServiceFinalizerName})
		// Update CR
		if err := r.client.Update(context.TODO(), cr); err != nil {
			reqLogger.Error(err, "Failed to update ExternalService with finalizer")
			return err
		}

	}
	return nil
}

// deleteResourceForExternalService deletes all related resource for the external service
func (r *ReconcileExternalService) deleteResourceForExternalService(cr *submarinerv1alpha1.ExternalService) error {
	// Delete pod
	pod := &corev1.Pod{}
	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: cr.Name, Namespace: ConnectorNamespace}, pod); err != nil && !errors.IsNotFound(err) {
		return err
	} else if err == nil {
		// Pod exists, so delete it
		_ = r.client.Delete(context.Background(), pod)
	}

	// Delete service
	svc := &corev1.Service{}
	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: cr.Name, Namespace: ConnectorNamespace}, svc); err != nil && !errors.IsNotFound(err) {
		return err
	} else if err == nil {
		// Service exists, so delete it
		_ = r.client.Delete(context.Background(), svc)
	}

	// Delete configmap
	configMap := &corev1.ConfigMap{}
	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: cr.Name, Namespace: ConnectorNamespace}, configMap); err != nil && !errors.IsNotFound(err) {
		return err
	} else if err == nil {
		// Configmap exists, so delete it
		_ = r.client.Delete(context.Background(), configMap)
	}

	return nil
}
