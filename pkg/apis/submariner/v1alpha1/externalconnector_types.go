package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ExternalConnectorSpec defines the desired state of ExternalConnector
type ExternalConnectorSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
	Version   string `json:"version,omitempty"`
	Namespace string `json:"namespace"`
}

// ExternalConnectorStatus defines the observed state of ExternalConnector
type ExternalConnectorStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ExternalConnector is the Schema for the externalconnectors API
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=externalconnectors,scope=Namespaced
type ExternalConnector struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ExternalConnectorSpec   `json:"spec,omitempty"`
	Status ExternalConnectorStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ExternalConnectorList contains a list of ExternalConnector
type ExternalConnectorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ExternalConnector `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ExternalConnector{}, &ExternalConnectorList{})
}
