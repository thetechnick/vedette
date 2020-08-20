/*
Copyright 2020 The Vedette authors.

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

// InterfaceProvisioner is the Schema for the interfaceprovisioners API.
// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="Priority",type="number",JSONPath=".priority"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:shortName=ip,categories=vedette
type InterfaceProvisioner struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Priority of this provisioner relative to other provisioners.
	// +kubebuilder:default=0
	Priority int `json:"priority,omitempty"`
	// Maximum capacity of a instances this provisioner can provide.
	MaxCapacity ResourceList `json:"maxCapacity,omitempty"`
	// Capabilities that this instance provisioner can provide.
	Capabilities CapabilitiesList `json:"capabilities"`
	// What happens with this Instance after it is released from a Claim.
	// +kubebuilder:default=Delete
	ReclaimPolicy InterfaceReclaimPolicy `json:"reclaimPolicy,omitempty"`

	// Provisioner Configuration

	// Custom provisioner outside of Vedette.
	// Vedette will just create a new InterfaceInstance with the provisioner set to the name of this object.
	// Further processing needs to be implemented in a custom controller.
	OutOfBand *OutOfBandProvisioner `json:"outOfBand,omitempty"`

	// The static provisioner will hand out the same secrets and configmaps to every Claim it encounters.
	Static *StaticProvisioner `json:"static,omitempty"`

	// The mapping provisioner creates a new instance of a custom object to fulfill a claim.
	// It can be used to offload work to other operators like KubeDB, without requiring the implementation of your own provisioner.
	Mapping *MappingProvisioner `json:"mapping,omitempty"`
}

// OutOfBandProvisioner defines the configuration of provider implementations outside of Vedette.
type OutOfBandProvisioner struct{}

// StaticProvisioner hands out the same configuration to all provisioned instances.
type StaticProvisioner struct {
	// Outputs is a list of Secrets or ConfigMaps that holds configuration and credentials to interact with this instance.
	Outputs []OutputReference `json:"outputs,omitempty"`
}

type MappingProvisioner struct {
	// List of objects to create, when provisioning an Instance.
	Objects []MappedObject `json:"objects"`
	// Outputs defines how to find outputs for the dynamic objects created.
	Outputs []MappingProvisionerOutput `json:"outputs"`
}

type MappedObject struct {
	// Template contains a go template that is executed to create the object.
	Template string `json:"template"`
	// ReadinessProbe tests the created object for readiness.
	ReadinessProbe *MappedObjectReadinessProbe `json:"readinessProbe,omitempty"`
}

type MappedObjectReadinessProbe struct {
	// JSONPath selecting a property to test for readyness.
	JSONPath string `json:"jsonPath"`
	// Regular Expression to test the property selected by the JSONPath.
	RegexTest string `json:"regexTest"`
}

type MappingProvisionerOutput struct {
	// Name of this output.
	Name string `json:"name"`
	// References a Secret in the same namespace.
	SecretRef *MappingProvisionerObjectReference `json:"secretRef,omitempty"`
	// References a ConfigMap in the same namespace.
	ConfigMapRef *MappingProvisionerObjectReference `json:"configMapRef,omitempty"`
	// Creates a new Secret via the given template.
	SecretTemplate MappingProvisionerOutputTemplate `json:"secretTemplate,omitempty"`
	// Creates a new ConfigMap via the given template.
	ConfigMapTemplate MappingProvisionerOutputTemplate `json:"configMapTemplate,omitempty"`
}

type MappingProvisionerObjectReference struct {
	NameTemplate string `json:"nameTemplate"`
}

// Map of keys to go templates to populate a Secret or ConfigMap with.
type MappingProvisionerOutputTemplate map[string]string

// InterfaceProvisionerList contains a list of InterfaceProvisioner
// +kubebuilder:object:root=true
type InterfaceProvisionerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InterfaceProvisioner `json:"items"`
}

func init() {
	SchemeBuilder.Register(&InterfaceProvisioner{}, &InterfaceProvisionerList{})
}
