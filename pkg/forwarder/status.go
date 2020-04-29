package forwarder

import (
	"github.com/golang/glog"
	"github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	clv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/client/clientset/versioned/typed/submariner/v1alpha1"
	"github.com/mkimuram/k8s-ext-connector/pkg/util"
	corev1 "k8s.io/api/core/v1"
)

func needSync(fwd *v1alpha1.Forwarder) bool {
	// Sync is needed if
	// - rule is not updating
	// - generations are different between rule and sync || not is not synced
	return fwd.Status.Conditions.IsFalseFor(v1alpha1.ConditionRuleUpdating) &&
		(fwd.Status.RuleGeneration != fwd.Status.SyncGeneration ||
			fwd.Status.Conditions.IsTrueFor(v1alpha1.ConditionRuleSyncing))
}

func needCheckSync(fwd *v1alpha1.Forwarder) bool {
	// CheckSync is needed if
	// - rule is not updating
	// - generations are the same between rule and sync
	// - rule is synced
	return fwd.Status.Conditions.IsFalseFor(v1alpha1.ConditionRuleUpdating) &&
		fwd.Status.RuleGeneration == fwd.Status.SyncGeneration &&
		fwd.Status.Conditions.IsFalseFor(v1alpha1.ConditionRuleSyncing)
}

func setSyncing(clientset clv1alpha1.SubmarinerV1alpha1Interface, ns string, fwd *v1alpha1.Forwarder) error {
	var err error
	if fwd.Status.Conditions.SetCondition(util.RuleSyncingCondition(corev1.ConditionTrue)) {
		fwd, err = clientset.Forwarders(ns).UpdateStatus(fwd)
		if err != nil {
			return err
		}
		glog.Infof("Update RuleSyncingCondition to true")
	}
	return nil
}

func setSynced(clientset clv1alpha1.SubmarinerV1alpha1Interface, ns string, fwd *v1alpha1.Forwarder) error {
	var err error
	if fwd.Status.Conditions.SetCondition(util.RuleSyncingCondition(corev1.ConditionFalse)) {
		fwd.Status.SyncGeneration = fwd.Status.RuleGeneration
		fwd, err = clientset.Forwarders(ns).UpdateStatus(fwd)
		if err != nil {
			return err
		}
		glog.Infof("Update RuleSyncingCondition to false")
	}
	return nil
}
