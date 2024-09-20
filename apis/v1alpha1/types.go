package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Pulpo is a specification for a Pulpo resource
type Pulpo struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PulpoSpec   `json:"spec"`
	Status PulpoStatus `json:"status"`
}

// PulpoSpec is the spec for a Pulpo resource
type PulpoSpec struct {
	Origen     string `json:"origen"`
	Tentaculos *int32 `json:"tentaculos"`
}

// PulpoStatus is the status for a Pulpo resource
type PulpoStatus struct {
	TentaculosDisponibles int32 `json:"tentaculosDisponibles"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PulpoList is a list of Pulpo resources
type PulpoList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Pulpo `json:"items"`
}
