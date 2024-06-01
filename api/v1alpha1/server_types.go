/*
Copyright 2024 joerx.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ServerSpec defines the desired state of Server
type ServerSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// +kubebuilder:validation:Enum=survival;creative;adventure
	// +kubebuilder:default=creative
	GameMode string `json:"gamemode"`

	// +kubebuilder:validation:Enum=peaceful;easy;normal;hard
	// +kubebuilder:default=peaceful
	Difficulty string `json:"difficulty"`

	// +kubebuilder:default=false
	Eula bool `json:"eula"`

	// +kubebuilder:validation:Enum=VANILLA
	// +kubebuilder:default=VANILLA
	ServerType string `json:"serverType"`

	// TODO: validate semver format
	// +kubebuilder:default=LATEST
	ServerVersion string `json:"serverVersion"`

	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:validation:Minimum=2
	// +kubebuilder:default=10
	MaxPlayers int32 `json:"maxPlayers"`

	// +kubebuilder:default=Welcome to Minecraft on Kubernetes!
	Motd string `json:"motd"`

	// Active decides wether the server is active. Inactive servers will be scaled to zero replicas
	// +kubebuilder:default=true
	Active bool `json:"active"`

	// Port defines the port that will be used to init the container with the image
	ContainerPort int32 `json:"containerPort,omitempty"`
}

// ServerStatus defines the observed state of Server
type ServerStatus struct {
	// Represents the observations of a Server's current state.
	// Server.status.conditions.type are: "Available", "Progressing", and "Degraded"
	// Server.status.conditions.status are one of True, False, Unknown.
	// Server.status.conditions.reason the value should be a CamelCase string and producers of specific
	// condition types may define expected values and meanings for this field, and whether the values
	// are considered a guaranteed API.
	// Server.status.conditions.Message is a human readable message indicating details about the transition.
	// For further information see: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Server is the Schema for the servers API
type Server struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServerSpec   `json:"spec,omitempty"`
	Status ServerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ServerList contains a list of Server
type ServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Server `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Server{}, &ServerList{})
}
