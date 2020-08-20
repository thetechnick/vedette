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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1alpha1 "github.com/thetechnick/vedette/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestInterfaceInstanceReconciler(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "admin-user-credentials",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"key1": []byte("val1"),
		},
	}
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "connection-data",
			Namespace: "default",
		},
		BinaryData: map[string][]byte{
			"key1": []byte("val1"),
		},
		Data: map[string]string{
			"key2": "val2",
		},
	}

	instance := &corev1alpha1.InterfaceInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test3000",
			Namespace: "default",
		},
		Spec: corev1alpha1.InterfaceInstanceSpec{
			Outputs: []corev1alpha1.OutputReference{
				{
					Name: "admin",
					SecretRef: &corev1alpha1.ObjectReference{
						Name: secret.Name,
					},
				},
				{
					Name: "connection",
					ConfigMapRef: &corev1alpha1.ObjectReference{
						Name: configMap.Name,
					},
				},
			},
		},
	}

	t.Run("staticProvisioner", func(t *testing.T) {
		client := NewClient()
		dw := NewDynamicWatcherMock()
		r := &InterfaceInstanceReconciler{
			Client:         client,
			Scheme:         testScheme,
			DynamicWatcher: dw,
		}
		ctx := context.Background()

		instance := instance.DeepCopy()
		instance.Spec.Outputs = nil
		provisioner := &corev1alpha1.InterfaceProvisioner{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "static",
				Namespace: "default",
			},
			Static: &corev1alpha1.StaticProvisioner{
				Outputs: []corev1alpha1.OutputReference{
					{
						Name: "output1",
						SecretRef: &corev1alpha1.ObjectReference{
							Name: "secret1",
						},
					},
				},
			},
		}

		// Mock
		var updatedInstance *corev1alpha1.InterfaceInstance
		client.
			On("Update", mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				obj := args.Get(1).(*corev1alpha1.InterfaceInstance)
				updatedInstance = obj
			}).
			Return(*new(error))
		client.StatusMock.
			On("Update", mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				obj := args.Get(1).(*corev1alpha1.InterfaceInstance)
				updatedInstance = obj
			}).
			Return(*new(error))

		err := r.staticProvisioner(ctx, instance.DeepCopy(), provisioner)
		require.NoError(t, err)

		if client.AssertCalled(t, "Update", mock.Anything, mock.AnythingOfType("*v1alpha1.InterfaceInstance"), mock.Anything) {
			assert.Equal(t, provisioner.Static.Outputs, updatedInstance.Spec.Outputs)
		}
	})

	t.Run("reconcileSecretTemplate", func(t *testing.T) {
		client := NewClient()
		dw := NewDynamicWatcherMock()
		r := &InterfaceInstanceReconciler{
			Client:         client,
			Scheme:         testScheme,
			DynamicWatcher: dw,
		}
		ctx := context.Background()
		output := corev1alpha1.MappingProvisionerOutput{
			Name: "output1",
			SecretTemplate: corev1alpha1.MappingProvisionerOutputTemplate{
				"key1": "property-{{.Name}}",
				"host": "{{.Name}}.{{.Namespace}}.svc.cluster.local",
			},
		}

		// Mock
		client.
			On("Get", mock.Anything, mock.Anything, mock.Anything).
			Return(errors.NewNotFound(schema.GroupResource{
				Group:    "",
				Resource: "secrets",
			}, instance.Name))
		client.
			On("Create", mock.Anything, mock.Anything, mock.Anything).
			Return(*new(error))

		// Test
		secret, err := r.reconcileSecretTemplate(ctx, instance, output)
		require.NoError(t, err)

		assert.Equal(t, instance.Name+"-output1", secret.Name)
		assert.Equal(t, map[string][]byte{
			"key1": []byte("property-test3000"),
			"host": []byte("test3000.default.svc.cluster.local"),
		}, secret.Data)
	})

	t.Run("reconcileConfigMapTemplate", func(t *testing.T) {
		client := NewClient()
		dw := NewDynamicWatcherMock()
		r := &InterfaceInstanceReconciler{
			Client:         client,
			Scheme:         testScheme,
			DynamicWatcher: dw,
		}
		ctx := context.Background()
		output := corev1alpha1.MappingProvisionerOutput{
			Name: "output1",
			ConfigMapTemplate: corev1alpha1.MappingProvisionerOutputTemplate{
				"key1": "property-{{.Name}}",
				"host": "{{.Name}}.{{.Namespace}}.svc.cluster.local",
			},
		}

		// Mock
		client.
			On("Get", mock.Anything, mock.Anything, mock.Anything).
			Return(errors.NewNotFound(schema.GroupResource{
				Group:    "",
				Resource: "configmaps",
			}, instance.Name))
		client.
			On("Create", mock.Anything, mock.Anything, mock.Anything).
			Return(*new(error))

		// Test
		configMap, err := r.reconcileConfigMapTemplate(ctx, instance, output)
		require.NoError(t, err)

		assert.Equal(t, instance.Name+"-output1", configMap.Name)
		assert.Equal(t, map[string]string{
			"key1": "property-test3000",
			"host": "test3000.default.svc.cluster.local",
		}, configMap.Data)
	})

	t.Run("checkOutputReferences", func(t *testing.T) {
		client := NewClient()
		dw := NewDynamicWatcherMock()
		r := &InterfaceInstanceReconciler{
			Client:         client,
			Scheme:         testScheme,
			DynamicWatcher: dw,
		}
		ctx := context.Background()

		// Mock
		var (
			updatedInstance  *corev1alpha1.InterfaceInstance
			updatedConfigMap *corev1.ConfigMap
			updatedSecret    *corev1.Secret
		)
		client.
			On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1.ConfigMap")).
			Run(func(args mock.Arguments) {
				obj := args.Get(2).(*corev1.ConfigMap)
				updatedConfigMap = obj
				configMap.DeepCopyInto(obj)
			}).
			Return(*new(error))
		client.
			On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1.Secret")).
			Run(func(args mock.Arguments) {
				obj := args.Get(2).(*corev1.Secret)
				updatedSecret = obj
				secret.DeepCopyInto(obj)
			}).
			Return(*new(error))
		client.
			On("List", mock.Anything, mock.Anything, mock.Anything).
			Return(*new(error))

		client.
			On("Update",
				mock.Anything, mock.AnythingOfType("*v1alpha1.InterfaceInstance"), mock.Anything).
			Return(*new(error))
		client.
			On("Update",
				mock.Anything, mock.AnythingOfType("*v1.ConfigMap"), mock.Anything).
			Return(*new(error))
		client.
			On("Update",
				mock.Anything, mock.AnythingOfType("*v1.Secret"), mock.Anything).
			Return(*new(error))
		client.StatusMock.
			On("Update",
				mock.Anything, mock.AnythingOfType("*v1alpha1.InterfaceInstance"), mock.Anything).
			Run(func(args mock.Arguments) {
				obj := args.Get(1).(*corev1alpha1.InterfaceInstance)
				updatedInstance = obj
			}).
			Return(*new(error))

		err := r.checkOutputReferences(ctx, instance)
		require.NoError(t, err)

		// Check back references are set
		if client.AssertCalled(t, "Update", mock.Anything, mock.AnythingOfType("*v1.ConfigMap"), mock.Anything) {
			assert.Equal(t, map[string]string{
				instanceLabelPrefix + instance.Name: "true",
			}, updatedConfigMap.Labels)
		}
		if client.AssertCalled(t, "Update", mock.Anything, mock.AnythingOfType("*v1.Secret"), mock.Anything) {
			assert.Equal(t, map[string]string{
				instanceLabelPrefix + instance.Name: "true",
			}, updatedSecret.Labels)
		}

		if assert.Len(t, updatedInstance.Status.Outputs, 2) {
			assert.NotEmpty(t, updatedInstance.Status.Outputs[0].Checksum)
			assert.NotEmpty(t, updatedInstance.Status.Outputs[1].Checksum)
		}
	})

	t.Run("reconcileMappedObject", func(t *testing.T) {
		mappedObject := corev1alpha1.MappedObject{
			Template: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{.Name}}
spec:
  selector:
    matchLabels:
      app: nginx
      instance: {{.Name}}
  template:
    metadata:
      labels:
        app: nginx
        instance: {{.Name}}
    spec:
      containers:
      - name: nginx
        image: nginx:1.14.2
        ports:
        - containerPort: 80`,
			ReadinessProbe: &corev1alpha1.MappedObjectReadinessProbe{
				JSONPath:  `{.status.conditions[?(@.type=="Available")].status}`,
				RegexTest: "True",
			},
		}

		t.Run("creates new object", func(t *testing.T) {
			dw := NewDynamicWatcherMock()
			client := NewClient()
			r := &InterfaceInstanceReconciler{
				Client:         client,
				Scheme:         testScheme,
				DynamicWatcher: dw,
			}
			ctx := context.Background()

			// Mock
			client.
				On("Get", mock.Anything, mock.Anything,
					mock.AnythingOfType("*unstructured.Unstructured")).
				Return(errors.NewNotFound(schema.GroupResource{
					Group:    "apps",
					Resource: "deployments",
				}, instance.Name))
			client.
				On("Create", mock.Anything,
					mock.AnythingOfType("*unstructured.Unstructured"), mock.Anything).
				Return(*new(error))

			dw.
				On("Watch", mock.Anything, mock.Anything).
				Return(*new(error))

			// Test
			ready, key, err := r.reconcileMappedObject(ctx, instance, mappedObject)
			require.NoError(t, err)
			assert.Equal(t, "apps/v1:Deployment default/test3000", key)
			assert.False(t, ready, "object should not be ready")
		})

		t.Run("updated existing object and checks readiness", func(t *testing.T) {
			dw := NewDynamicWatcherMock()
			client := NewClient()
			r := &InterfaceInstanceReconciler{
				Client:         client,
				Scheme:         testScheme,
				DynamicWatcher: dw,
			}
			ctx := context.Background()

			existingObj := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]interface{}{
						"name":      instance.Name,
						"namespace": instance.Namespace,
					},
					"spec": map[string]interface{}{
						"selector": map[string]interface{}{
							"matchLabels": map[string]interface{}{
								"app":      "nginx",
								"instance": instance.Name,
							},
						},
						"template": map[string]interface{}{
							"metadata": map[string]interface{}{
								"labels": map[string]interface{}{
									"app":      "nginx",
									"instance": instance.Name,
								},
							},
							"spec": map[string]interface{}{},
						},
					},
					"status": map[string]interface{}{
						"conditions": []interface{}{
							map[string]interface{}{
								"type":   "Available",
								"status": "True",
							},
						},
					},
				},
			}

			// Mock
			client.
				On("Get", mock.Anything, mock.Anything,
					mock.AnythingOfType("*unstructured.Unstructured")).
				Return(*new(error))
			client.
				On("Update", mock.Anything,
					mock.AnythingOfType("*unstructured.Unstructured"), mock.Anything).
				Run(func(args mock.Arguments) {
					obj := args.Get(1).(*unstructured.Unstructured)
					existingObj.DeepCopyInto(obj)
				}).
				Return(*new(error))

			dw.
				On("Watch", mock.Anything, mock.Anything).
				Return(*new(error))

			// Test
			ready, key, err := r.reconcileMappedObject(ctx, instance, mappedObject)
			require.NoError(t, err)
			assert.Equal(t, "apps/v1:Deployment default/test3000", key)
			assert.True(t, ready, "object should be ready")
		})
	})
}

func Test_instanceLabelPrefixMapper(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "admin-user-credentials",
			Namespace: "default",
			Labels: map[string]string{
				"vedette.io/instance-test-123": "true",
				"vedette.io/instance-test-456": "true",
			},
		},
		Data: map[string][]byte{
			"key1": []byte("val1"),
		},
	}

	requests := instanceLabelPrefixMapper.ToRequests.Map(handler.MapObject{
		Meta:   secret,
		Object: secret,
	})
	assert.Contains(t, requests, reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-123",
			Namespace: "default",
		},
	})
	assert.Contains(t, requests, reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-456",
			Namespace: "default",
		},
	})
}

func Test_overrideFields(t *testing.T) {
	tests := []struct {
		name              string
		src, dest, expect *unstructured.Unstructured
	}{
		{
			name: "",
			src: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Deployment",
					"spec": map[string]interface{}{
						"prop1": "val1",
						"prop2": "val2",
					},
				},
			},
			dest: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Deployment",
					"spec": map[string]interface{}{
						"prop1": "should_be_overridden",
						"prop2": "should_be_overridden",
						"prop3": "val3",
					},
					"status": map[string]interface{}{
						"prop1": "val1",
						"prop2": "val2",
					},
				},
			},
			expect: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Deployment",
					"spec": map[string]interface{}{
						"prop1": "val1",
						"prop2": "val2",
						"prop3": "val3",
					},
					"status": map[string]interface{}{
						"prop1": "val1",
						"prop2": "val2",
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			overrideFields(test.src, test.dest)
			assert.Equal(t, test.expect, test.dest)
		})
	}
}
