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

package controllers

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1alpha1 "github.com/thetechnick/vedette/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestInterfaceClaimReconciler_reconcileInputs(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "admin-user-credentials",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"key1": []byte("val1"),
			"key2": []byte("val2"),
		},
	}
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "connection-data",
			Namespace: "default",
		},
		BinaryData: map[string][]byte{
			"key1": []byte("val1"),
			"key2": []byte("val2"),
		},
		Data: map[string]string{
			"key3": "val3",
			"key4": "val4",
		},
	}
	instance := &corev1alpha1.InterfaceInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-instance",
			Namespace: "default",
		},
		Spec: corev1alpha1.InterfaceInstanceSpec{
			Outputs: []corev1alpha1.OutputReference{
				{
					Name: "admin-user",
					SecretRef: &corev1alpha1.ObjectReference{
						Name: "admin-user-credentials",
					},
				},
				{
					Name: "connection",
					ConfigMapRef: &corev1alpha1.ObjectReference{
						Name: "connection-data",
					},
				},
			},
		},
	}
	claim := &corev1alpha1.InterfaceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-claim",
			Namespace: "default",
		},
		Spec: corev1alpha1.InterfaceClaimSpec{
			Inputs: []corev1alpha1.InputReference{
				{
					Name: "admin-user",
					SecretRef: &corev1alpha1.ObjectReference{
						Name: "test-claim-user",
					},
				},
				{
					Name: "connection",
					ConfigMapRef: &corev1alpha1.ObjectReference{
						Name: "test-claim-connection",
					},
				},
			},
		},
	}

	t.Run("creates Secrets and ConfigMaps", func(t *testing.T) {
		client := NewClient()
		r := &InterfaceClaimReconciler{
			Client: client,
			Scheme: testScheme,
		}
		ctx := context.Background()

		// Mock
		client.
			On("Get", mock.Anything, types.NamespacedName{
				Name:      secret.Name,
				Namespace: "default",
			}, mock.Anything).
			Run(func(args mock.Arguments) {
				obj := args.Get(2).(*corev1.Secret)
				secret.DeepCopyInto(obj)
			}).
			Return(*new(error))

		client.
			On("Get", mock.Anything, types.NamespacedName{
				Name:      configMap.Name,
				Namespace: "default",
			}, mock.Anything).
			Run(func(args mock.Arguments) {
				obj := args.Get(2).(*corev1.ConfigMap)
				configMap.DeepCopyInto(obj)
			}).
			Return(*new(error))

		client.
			On("Create", mock.Anything, mock.Anything, mock.Anything).
			Return(*new(error))

		// Run
		err := r.reconcileInputs(ctx, claim, instance, nil, nil)
		require.NoError(t, err)

		// Asserts
		client.AssertExpectations(t)
		client.AssertCalled(
			t, "Create",
			mock.Anything, mock.MatchedBy(func(s *corev1.Secret) bool {
				return s.Name == "test-claim-user" &&
					assert.ObjectsAreEqual(s.Data, secret.Data)
			}), mock.Anything)

		client.AssertCalled(
			t, "Create",
			mock.Anything, mock.MatchedBy(func(cm *corev1.ConfigMap) bool {
				return cm.Name == "test-claim-connection" &&
					assert.ObjectsAreEqual(cm.Data, configMap.Data) &&
					assert.ObjectsAreEqual(cm.BinaryData, configMap.BinaryData)
			}), mock.Anything)
	})

	t.Run("updates Secrets and ConfigMaps", func(t *testing.T) {
		client := NewClient()
		r := &InterfaceClaimReconciler{
			Client: client,
			Scheme: testScheme,
		}
		ctx := context.Background()

		// Objects
		existingSecret := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-claim-user",
				Namespace: "default",
			},
			Data: map[string][]byte{
				"key1": []byte("val1"),
			},
		}
		existingConfigMap := corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-claim-connection",
				Namespace: "default",
			},
			BinaryData: map[string][]byte{
				"key1": []byte("val1"),
			},
			Data: map[string]string{
				"key3": "val3",
			},
		}

		// Mock
		client.
			On("Get", mock.Anything, types.NamespacedName{
				Name:      secret.Name,
				Namespace: "default",
			}, mock.Anything).
			Run(func(args mock.Arguments) {
				obj := args.Get(2).(*corev1.Secret)
				secret.DeepCopyInto(obj)
			}).
			Return(*new(error))

		client.
			On("Get", mock.Anything, types.NamespacedName{
				Name:      configMap.Name,
				Namespace: "default",
			}, mock.Anything).
			Run(func(args mock.Arguments) {
				obj := args.Get(2).(*corev1.ConfigMap)
				configMap.DeepCopyInto(obj)
			}).
			Return(*new(error))

		client.
			On("Update", mock.Anything, mock.Anything, mock.Anything).
			Return(*new(error))

		// Run
		err := r.reconcileInputs(
			ctx, claim, instance,
			[]corev1.Secret{existingSecret},
			[]corev1.ConfigMap{existingConfigMap})
		require.NoError(t, err)

		// Asserts
		client.AssertExpectations(t)
		client.AssertCalled(
			t, "Update",
			mock.Anything, mock.MatchedBy(func(s *corev1.Secret) bool {
				return s.Name == "test-claim-user" &&
					assert.ObjectsAreEqual(s.Data, secret.Data)
			}), mock.Anything)

		client.AssertCalled(
			t, "Update",
			mock.Anything, mock.MatchedBy(func(cm *corev1.ConfigMap) bool {
				return cm.Name == "test-claim-connection" &&
					assert.ObjectsAreEqual(cm.Data, configMap.Data) &&
					assert.ObjectsAreEqual(cm.BinaryData, configMap.BinaryData)
			}), mock.Anything)
	})
}

func Test_byLeastCapacity(t *testing.T) {
	// Simple test
	instance1 := corev1alpha1.InterfaceInstance{
		Spec: corev1alpha1.InterfaceInstanceSpec{
			Capacity: corev1alpha1.ResourceList{
				"storage": resource.MustParse("1Gi"),
			},
		},
	}
	instance2 := corev1alpha1.InterfaceInstance{
		Spec: corev1alpha1.InterfaceInstanceSpec{
			Capacity: corev1alpha1.ResourceList{
				"storage": resource.MustParse("10Gi"),
			},
		},
	}
	instance3 := corev1alpha1.InterfaceInstance{
		Spec: corev1alpha1.InterfaceInstanceSpec{
			Capacity: corev1alpha1.ResourceList{
				"storage": resource.MustParse("3Gi"),
			},
		},
	}

	// multiple properties
	instance11 := corev1alpha1.InterfaceInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name: "instance-11",
		},
		Spec: corev1alpha1.InterfaceInstanceSpec{
			Capacity: corev1alpha1.ResourceList{
				"storage":   resource.MustParse("1Gi"),
				"bandwidth": resource.MustParse("100Mi"),
				"cpu":       resource.MustParse("200m"),
				"memory":    resource.MustParse("2Gi"),
			},
		},
	}
	instance12 := corev1alpha1.InterfaceInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name: "instance-12",
		},
		Spec: corev1alpha1.InterfaceInstanceSpec{
			Capacity: corev1alpha1.ResourceList{
				"storage":   resource.MustParse("1Gi"),
				"bandwidth": resource.MustParse("200Mi"), // most
				"cpu":       resource.MustParse("200m"),
				"memory":    resource.MustParse("3Gi"), // most
			},
		},
	}
	instance13 := corev1alpha1.InterfaceInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name: "instance-13",
		},
		Spec: corev1alpha1.InterfaceInstanceSpec{
			Capacity: corev1alpha1.ResourceList{
				"storage":   resource.MustParse("2Gi"), // most
				"bandwidth": resource.MustParse("100Mi"),
				"cpu":       resource.MustParse("200m"),
				"memory":    resource.MustParse("2Gi"),
			},
		},
	}

	tests := []struct {
		name     string
		in       []corev1alpha1.InterfaceInstance
		expected []corev1alpha1.InterfaceInstance
	}{
		{
			name: "simple",
			in: []corev1alpha1.InterfaceInstance{
				instance2, instance3, instance1,
			},
			expected: []corev1alpha1.InterfaceInstance{
				instance1, instance3, instance2,
			},
		},
		{
			name: "multiple-parameters",
			in: []corev1alpha1.InterfaceInstance{
				instance12, instance13, instance11,
			},
			expected: []corev1alpha1.InterfaceInstance{
				instance11, instance13, instance12,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sort.Sort(byLeastCapacity(test.in))
			var sortedNames, expectedNames []string
			for _, i := range test.in {
				sortedNames = append(sortedNames, i.Name)
			}
			for _, i := range test.expected {
				expectedNames = append(expectedNames, i.Name)
			}
			assert.Equal(t, expectedNames, sortedNames)
		})
	}

}
