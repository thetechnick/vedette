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
	"k8s.io/apimachinery/pkg/api/resource"
)

// ConditionStatus represents a the state of a condition.
type ConditionStatus string

const (
	// ConditionTrue represents the fact that a given condition is true.
	ConditionTrue ConditionStatus = "True"

	// ConditionFalse represents the fact that a given condition is false.
	ConditionFalse ConditionStatus = "False"

	// ConditionUnknown represents the fact that a given condition is unknown.
	ConditionUnknown ConditionStatus = "Unknown"
)

// ObjectReference references another object in the same Namespace.
type ObjectReference struct {
	Name string `json:"name"`
}

type ResourceList map[string]resource.Quantity

// +kubebuilder:validation:MinItems=1
type CapabilitiesList []string

type InterfaceReclaimPolicy string

const (
	InterfaceReclaimPolicyDelete InterfaceReclaimPolicy = "Delete"
	InterfaceReclaimPolicyRetain InterfaceReclaimPolicy = "Retain"
)
