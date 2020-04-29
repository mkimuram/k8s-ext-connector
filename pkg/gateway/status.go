package gateway

import (
	"github.com/golang/glog"
	"github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	clv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/client/clientset/versioned/typed/submariner/v1alpha1"
	"github.com/mkimuram/k8s-ext-connector/pkg/util"
	corev1 "k8s.io/api/core/v1"
)

func needSync(gw *v1alpha1.Gateway) bool {
	// Sync is needed if
	// - rule is not updating
	// - generations are different between rule and sync || not is not synced
	return gw.Status.Conditions.IsFalseFor(v1alpha1.ConditionRuleUpdating) &&
		(gw.Status.RuleGeneration != gw.Status.SyncGeneration ||
			gw.Status.Conditions.IsTrueFor(v1alpha1.ConditionRuleSyncing))
}

func needCheckSync(gw *v1alpha1.Gateway) bool {
	// CheckSync is needed if
	// - rule is not updating
	// - generations are the same between rule and sync
	// - rule is synced
	return gw.Status.Conditions.IsFalseFor(v1alpha1.ConditionRuleUpdating) &&
		gw.Status.RuleGeneration == gw.Status.SyncGeneration &&
		gw.Status.Conditions.IsFalseFor(v1alpha1.ConditionRuleSyncing)
}

func setSyncing(clientset clv1alpha1.SubmarinerV1alpha1Interface, ns string, gw *v1alpha1.Gateway) error {
	var err error
	if gw.Status.Conditions.SetCondition(util.RuleSyncingCondition(corev1.ConditionTrue)) {
		gw, err = clientset.Gateways(ns).UpdateStatus(gw)
		if err != nil {
			return err
		}
		glog.Infof("Update RuleSyncingCondition to true")
	}
	return nil
}

func setSynced(clientset clv1alpha1.SubmarinerV1alpha1Interface, ns string, gw *v1alpha1.Gateway) error {
	var err error
	if gw.Status.Conditions.SetCondition(util.RuleSyncingCondition(corev1.ConditionFalse)) {
		gw.Status.SyncGeneration = gw.Status.RuleGeneration
		gw, err = clientset.Gateways(ns).UpdateStatus(gw)
		if err != nil {
			return err
		}
		glog.Infof("Update RuleSyncingCondition to false")
	}
	return nil
}
