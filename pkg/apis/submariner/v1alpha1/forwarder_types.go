package v1alpha1

import (
	"github.com/operator-framework/operator-sdk/pkg/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file

// ForwarderSpec defines the desired state of Forwarder
type ForwarderSpec struct {
	EgressRules  []ForwarderRule `json:"egressrules"`
	IngressRules []ForwarderRule `json:"ingressrules"`
}

type ForwarderRule struct {
	Protocol    string     `json:"protocol,omitempty"`
	SourceIP    string     `json:"sourceip,omitempty"`
	ForwardPort string     `json:"forwardport,omitempty"`
	TargetIP    string     `json:"targetip,omitempty"`
	TargetPort  string     `json:"targetport,omitempty"`
	Gateway     GatewayRef `json:"gateway"`
}

type GatewayRef struct {
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
}

// ForwarderStatus defines the observed state of Forwarder
type ForwarderStatus struct {
	Conditions     status.Conditions `json:"conditions"`
	ForwarderIP    string            `json:"forwarderip,omitempty"`
	RuleGeneration int               `json:"rulegeneration,omitempty"`
}

const (
	ConditionRuleUpdating status.ConditionType = "RuleUpdating"
	ConditionRuleSyncing  status.ConditionType = "RuleSyncing"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient

// Forwarder is the Schema for the forwarders API
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=forwarders,scope=Namespaced
type Forwarder struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ForwarderSpec   `json:"spec,omitempty"`
	Status ForwarderStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ForwarderList contains a list of Forwarder
type ForwarderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Forwarder `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Forwarder{}, &ForwarderList{})
}
