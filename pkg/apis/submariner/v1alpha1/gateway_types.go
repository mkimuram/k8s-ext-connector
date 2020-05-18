package v1alpha1

import (
	"github.com/operator-framework/operator-sdk/pkg/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file

// GatewaySpec defines the desired state of Gateway
type GatewaySpec struct {
	EgressRules  []GatewayRule `json:"egressrules"`
	IngressRules []GatewayRule `json:"ingressrules"`
	GatewayIP    string        `json:"gatewayip,omitempty"`
}

type GatewayRule struct {
	Protocol        string       `json:"protocol,omitempty"`
	SourceIP        string       `json:"sourceip,omitempty"`
	TargetPort      string       `json:"targetport,omitempty"`
	DestinationPort string       `json:"destinationport,omitempty"`
	DestinationIP   string       `json:"destinationip,omitempty"`
	Forwarder       ForwarderRef `json:"forwarder"`
	ForwarderIP     string       `json:"forwarderip,omitempty"`
	RelayPort       string       `json:"relayport,omitempty"`
}

type ForwarderRef struct {
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
}

// GatewayStatus defines the observed state of Gateway
type GatewayStatus struct {
	Conditions     status.Conditions `json:"conditions"`
	RuleGeneration int               `json:"rulegeneration,omitempty"`
	SyncGeneration int               `json:"syncgeneration,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient

// Gateway is the Schema for the gateways API
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=gateways,scope=Namespaced
type Gateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GatewaySpec   `json:"spec,omitempty"`
	Status GatewayStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GatewayList contains a list of Gateway
type GatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Gateway `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Gateway{}, &GatewayList{})
}
