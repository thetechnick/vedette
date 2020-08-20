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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"text/template"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/jsonpath"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"sigs.k8s.io/yaml"

	corev1alpha1 "github.com/thetechnick/vedette/api/v1alpha1"
)

const (
	instanceLabelPrefix         = "vedette.io/instance-"
	instanceReferencesFinalizer = "vedette.io/references"
)

// InterfaceInstanceReconciler reconciles a InterfaceInstance object
type InterfaceInstanceReconciler struct {
	client.Client
	Log            logr.Logger
	Scheme         *runtime.Scheme
	DynamicWatcher WatcherInterface
}

// +kubebuilder:rbac:groups=core.vedette.io,resources=interfaceinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.vedette.io,resources=interfaceinstances/status,verbs=get;update;patch

func (r *InterfaceInstanceReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("interfaceinstance", req.NamespacedName)
	log.Info("starting")

	instance := &corev1alpha1.InterfaceInstance{}
	if errors.IsNotFound(r.Get(ctx, req.NamespacedName, instance)) {
		return ctrl.Result{}, nil
	}

	// Add finalizer
	finalizers := sets.NewString(instance.Finalizers...)
	if !finalizers.Has(instanceReferencesFinalizer) {
		finalizers.Insert(instanceReferencesFinalizer)
		instance.Finalizers = finalizers.List()
		if err := r.Update(ctx, instance); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
	}

	// Deleted
	if !instance.DeletionTimestamp.IsZero() {
		if err := r.DynamicWatcher.Free(instance); err != nil {
			return ctrl.Result{}, fmt.Errorf("freeing watcher: %w", err)
		}
		if err := r.removeStaleBackReferences(ctx, instance, nil, nil); err != nil {
			return ctrl.Result{}, fmt.Errorf("removing all references: %w", err)
		}
		finalizers := sets.NewString(instance.Finalizers...)
		finalizers.Delete(instanceReferencesFinalizer)
		instance.Finalizers = finalizers.List()
		if err := r.Update(ctx, instance); err != nil {
			return ctrl.Result{}, fmt.Errorf("removing finalizers: %w", err)
		}
		return ctrl.Result{}, nil
	}

	// Provisioner Handling
	if stop, err := r.provision(ctx, instance); err != nil {
		return ctrl.Result{}, err
	} else if stop {
		return ctrl.Result{}, nil
	}

	// Checksum handling
	if err := r.checkOutputReferences(ctx, instance); err != nil {
		return ctrl.Result{}, fmt.Errorf("checking outputs: %w", err)
	}

	// Check for deletion
	if stop, err := r.checkForCleanup(ctx, instance); err != nil {
		return ctrl.Result{}, err
	} else if stop {
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

// Check wether outputs exist and report a checksum of each output.
func (r *InterfaceInstanceReconciler) checkOutputReferences(
	ctx context.Context,
	instance *corev1alpha1.InterfaceInstance,
) error {
	instance.Status.Outputs = nil
	var (
		knownConfigMapNames = map[string]struct{}{}
		knownSecretNames    = map[string]struct{}{}
	)
	for _, output := range instance.Spec.Outputs {
		var data map[string][]byte
		switch {
		case output.ConfigMapRef != nil:
			configMap := &corev1.ConfigMap{}
			if err := r.Get(ctx, types.NamespacedName{
				Name:      output.ConfigMapRef.Name,
				Namespace: instance.Namespace,
			}, configMap); err != nil {
				return fmt.Errorf("getting ConfigMap: %w", err)
			}

			// Setup a label for event-mapping
			if _, ok := configMap.Labels[instanceLabelPrefix+instance.Name]; !ok {
				if configMap.Labels == nil {
					configMap.Labels = map[string]string{}
				}
				configMap.Labels[instanceLabelPrefix+instance.Name] = "true"
				if err := r.Update(ctx, configMap); err != nil {
					return fmt.Errorf("set reference label on ConfigMap: %w", err)
				}
			}

			// Remember, that we want to continue referencing this ConfigMap
			knownConfigMapNames[configMap.Name] = struct{}{}

			data = map[string][]byte{}
			for k, v := range configMap.BinaryData {
				data[k] = v
			}
			for k, v := range configMap.Data {
				data[k] = []byte(v)
			}

		case output.SecretRef != nil:
			secret := &corev1.Secret{}
			if err := r.Get(ctx, types.NamespacedName{
				Name:      output.SecretRef.Name,
				Namespace: instance.Namespace,
			}, secret); err != nil {
				return fmt.Errorf("getting Secret: %w", err)
			}

			// Setup a label for event-mapping
			if _, ok := secret.Labels[instanceLabelPrefix+instance.Name]; !ok {
				if secret.Labels == nil {
					secret.Labels = map[string]string{}
				}
				secret.Labels[instanceLabelPrefix+instance.Name] = "true"
				if err := r.Update(ctx, secret); err != nil {
					return fmt.Errorf("set reference label on Secret: %w", err)
				}
			}

			// Remember, that we want to continue referencing this Secret
			knownSecretNames[secret.Name] = struct{}{}

			data = secret.Data
		}

		j, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("marshal json: %w", err)
		}
		checksum := sha256.Sum256(j)
		instance.Status.Outputs = append(instance.Status.Outputs, corev1alpha1.OutputStatus{
			Name:     output.Name,
			Checksum: checksum[:],
		})
	}
	if err := r.removeStaleBackReferences(ctx, instance, knownConfigMapNames, knownSecretNames); err != nil {
		return fmt.Errorf("remove stale back references: %w", err)
	}
	if err := r.Status().Update(ctx, instance); err != nil {
		return fmt.Errorf("updating Status: %w", err)
	}
	return nil
}

// Cleanup all outdated back references.
func (r *InterfaceInstanceReconciler) removeStaleBackReferences(
	ctx context.Context,
	instance *corev1alpha1.InterfaceInstance,
	knownConfigMapNames, knownSecretNames map[string]struct{},
) error {
	// ConfigMaps
	configMapList := &corev1.ConfigMapList{}
	if err := r.List(ctx, configMapList, client.MatchingLabels{
		instanceLabelPrefix + instance.Name: "True",
	}); err != nil {
		return fmt.Errorf("listing ConfigMaps: %w", err)
	}
	for _, configMap := range configMapList.Items {
		if _, ok := knownConfigMapNames[configMap.Name]; ok {
			continue
		}
		delete(configMap.Labels, instanceLabelPrefix+instance.Name)
		if err := r.Update(ctx, &configMap); err != nil {
			return fmt.Errorf("removing reference label on ConfigMap: %w", err)
		}
	}

	// Secrets
	secretList := &corev1.SecretList{}
	if err := r.List(ctx, secretList, client.MatchingLabels{
		instanceLabelPrefix + instance.Name: "True",
	}); err != nil {
		return fmt.Errorf("listing Secrets: %w", err)
	}
	for _, secret := range secretList.Items {
		if _, ok := knownSecretNames[secret.Name]; ok {
			continue
		}
		delete(secret.Labels, instanceLabelPrefix+instance.Name)
		if err := r.Update(ctx, &secret); err != nil {
			return fmt.Errorf("removing reference label on Secret: %w", err)
		}
	}
	return nil
}

// Check if the Claim belonging to this Instance still exists or if it was deleted.
func (r *InterfaceInstanceReconciler) provision(
	ctx context.Context,
	instance *corev1alpha1.InterfaceInstance,
) (stop bool, err error) {
	if instance.Spec.ProvisionerName == "" {
		// No provisioner nothing to do
		return false, nil
	}

	provisioner := &corev1alpha1.InterfaceProvisioner{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      instance.Spec.ProvisionerName,
		Namespace: instance.Namespace,
	}, provisioner); err != nil {
		return false, fmt.Errorf("getting provisioner: %w", err)
	}

	switch {
	case provisioner.OutOfBand != nil:
		// no touchy
		return false, nil
	case provisioner.Static != nil:
		return false, r.staticProvisioner(ctx, instance, provisioner)
	case provisioner.Mapping != nil:
		return false, r.mappingProvisioner(ctx, instance, provisioner)
	}
	return false, nil
}

// Check if the Claim belonging to this Instance still exists or if it was deleted.
func (r *InterfaceInstanceReconciler) checkForCleanup(
	ctx context.Context,
	instance *corev1alpha1.InterfaceInstance,
) (stop bool, err error) {
	if instance.Spec.Claim == nil &&
		instance.Status.GetCondition(corev1alpha1.InterfaceInstanceBound).Status != corev1alpha1.ConditionTrue {
		// Instance is not yet bound -> nothing to check
		return false, nil
	}

	claim := &corev1alpha1.InterfaceClaim{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      instance.Spec.Claim.Name,
		Namespace: instance.Namespace,
	}, claim); err == nil {
		// claim is present
		instance.Status.SetCondition(corev1alpha1.InterfaceInstanceCondition{
			Type:    corev1alpha1.InterfaceInstanceBound,
			Status:  corev1alpha1.ConditionTrue,
			Reason:  "Bound",
			Message: "Bound to instance.",
		})
		if err = r.Status().Update(ctx, instance); err != nil {
			return false, fmt.Errorf("update claim status: %w", err)
		}

		return true, nil
	} else if !errors.IsNotFound(err) {
		// some other error
		return false, fmt.Errorf("getting claim: %w", err)
	}

	// Claim was not found
	switch instance.Spec.ReclaimPolicy {
	case corev1alpha1.InterfaceReclaimPolicyDelete:
		if err := r.Delete(ctx, instance); err != nil {
			return false, fmt.Errorf("enforcing reclaim policy, deleting instance: %w", err)
		}
		return true, nil

	case corev1alpha1.InterfaceReclaimPolicyRetain:
		instance.Status.SetCondition(corev1alpha1.InterfaceInstanceCondition{
			Type:    corev1alpha1.InterfaceInstanceBound,
			Status:  corev1alpha1.ConditionFalse,
			Reason:  "Released",
			Message: fmt.Sprintf("The previously bound claim %s has been deleted.", instance.Spec.Claim.Name),
		})
		if err := r.Status().Update(ctx, instance); err != nil {
			return false, fmt.Errorf("updating instance status: %w", err)
		}
		delete(instance.Labels, claimLabel)
		instance.Spec.Claim.Name = ""
		if err := r.Update(ctx, instance); err != nil {
			return false, fmt.Errorf("updating instance: %w", err)
		}

		return true, nil
	default:
		// unknown ReclaimPolicy -> do nothing
		return false, nil
	}
}

func (r *InterfaceInstanceReconciler) mappingProvisioner(
	ctx context.Context,
	instance *corev1alpha1.InterfaceInstance,
	provisioner *corev1alpha1.InterfaceProvisioner,
) error {
	// Reconcile Objects
	var unreadyObjectKeys []string
	for i, mappedObject := range provisioner.Mapping.Objects {
		ready, key, err := r.reconcileMappedObject(ctx, instance, mappedObject)
		if err != nil {
			return fmt.Errorf("reconciling object [%d]: %w", i, err)
		}
		if !ready {
			unreadyObjectKeys = append(unreadyObjectKeys, key)
		}
	}
	if len(unreadyObjectKeys) > 0 {
		// Not everything is ready
		instance.Status.SetCondition(corev1alpha1.InterfaceInstanceCondition{
			Type:    corev1alpha1.InterfaceInstanceReady,
			Status:  corev1alpha1.ConditionFalse,
			Reason:  "ObjectsUnready",
			Message: fmt.Sprintf("Created objects are unready: %s.", strings.Join(unreadyObjectKeys, ", ")),
		})
		if err := r.Status().Update(ctx, instance); err != nil {
			return fmt.Errorf("updating Instance Status: %w", err)
		}
		return nil
	}

	// Reconcile Outputs
	var outputs []corev1alpha1.OutputReference
	for _, output := range provisioner.Mapping.Outputs {
		switch {
		case output.ConfigMapRef != nil:
			name, err := stringFromTemplate(output.ConfigMapRef.NameTemplate, instance)
			if err != nil {
				return fmt.Errorf("templating name for output %q: %w", output.Name, err)
			}
			outputs = append(outputs, corev1alpha1.OutputReference{
				Name: output.Name,
				ConfigMapRef: &corev1alpha1.ObjectReference{
					Name: name,
				},
			})

		case output.SecretRef != nil:
			name, err := stringFromTemplate(output.SecretRef.NameTemplate, instance)
			if err != nil {
				return fmt.Errorf("templating name for output %q: %w", output.Name, err)
			}
			outputs = append(outputs, corev1alpha1.OutputReference{
				Name: output.Name,
				SecretRef: &corev1alpha1.ObjectReference{
					Name: name,
				},
			})

		case output.ConfigMapTemplate != nil:
			configMap, err := r.reconcileConfigMapTemplate(ctx, instance, output)
			if err != nil {
				return fmt.Errorf("reconcile output %q: %w", output.Name, err)
			}
			outputs = append(outputs, corev1alpha1.OutputReference{
				Name: output.Name,
				ConfigMapRef: &corev1alpha1.ObjectReference{
					Name: configMap.Name,
				},
			})

		case output.SecretTemplate != nil:
			secret, err := r.reconcileSecretTemplate(ctx, instance, output)
			if err != nil {
				return fmt.Errorf("reconcile output %q: %w", output.Name, err)
			}
			outputs = append(outputs, corev1alpha1.OutputReference{
				Name: output.Name,
				SecretRef: &corev1alpha1.ObjectReference{
					Name: secret.Name,
				},
			})
		}
	}
	instance.Spec.Outputs = outputs
	if err := r.Update(ctx, instance); err != nil {
		return fmt.Errorf("updating InterfaceInstance: %w", err)
	}

	instance.Status.SetCondition(corev1alpha1.InterfaceInstanceCondition{
		Type:    corev1alpha1.InterfaceInstanceReady,
		Status:  corev1alpha1.ConditionTrue,
		Reason:  "ObjectsReady",
		Message: "All created objects are considered ready.",
	})
	if instance.Status.GetCondition(corev1alpha1.InterfaceInstanceAvailable).Status != corev1alpha1.ConditionTrue {
		instance.Status.SetCondition(corev1alpha1.InterfaceInstanceCondition{
			Type:    corev1alpha1.InterfaceInstanceAvailable,
			Status:  corev1alpha1.ConditionTrue,
			Reason:  "ObjectsReady",
			Message: "All created objects where considered ready, at least once before.",
		})
	}
	if err := r.Status().Update(ctx, instance); err != nil {
		return fmt.Errorf("updating Instance Status: %w", err)
	}
	return nil
}

func (r *InterfaceInstanceReconciler) reconcileSecretTemplate(
	ctx context.Context, instance *corev1alpha1.InterfaceInstance,
	output corev1alpha1.MappingProvisionerOutput,
) (*corev1.Secret, error) {
	data, err := mapFromMapTemplate(output.SecretTemplate, instance)
	if err != nil {
		return nil, fmt.Errorf("templating data: %w", err)
	}
	binaryData := map[string][]byte{}
	for k, v := range data {
		binaryData[k] = []byte(v)
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name + "-" + output.Name,
			Namespace: instance.Namespace,
		},
		Data: binaryData,
	}
	if err := controllerutil.SetControllerReference(instance, secret, r.Scheme); err != nil {
		return nil, fmt.Errorf("setting controller reference: %w", err)
	}

	existingSecret := &corev1.Secret{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      secret.Name,
		Namespace: secret.Namespace,
	}, existingSecret)
	if err != nil && !errors.IsNotFound(err) {
		return nil, fmt.Errorf("getting Secret: %w", err)
	}
	if errors.IsNotFound(err) {
		if err := r.Create(ctx, secret); err != nil {
			return nil, fmt.Errorf("creating Secret: %w", err)
		}
		return secret, nil
	}
	existingSecret.Data = secret.Data
	if err := r.Update(ctx, existingSecret); err != nil {
		return nil, fmt.Errorf("updating Secret: %w", err)
	}
	return existingSecret, nil
}

func (r *InterfaceInstanceReconciler) reconcileConfigMapTemplate(
	ctx context.Context, instance *corev1alpha1.InterfaceInstance,
	output corev1alpha1.MappingProvisionerOutput,
) (*corev1.ConfigMap, error) {
	data, err := mapFromMapTemplate(output.ConfigMapTemplate, instance)
	if err != nil {
		return nil, fmt.Errorf("templating data: %w", err)
	}
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name + "-" + output.Name,
			Namespace: instance.Namespace,
		},
		Data: data,
	}
	if err := controllerutil.SetControllerReference(instance, configMap, r.Scheme); err != nil {
		return nil, fmt.Errorf("setting controller reference: %w", err)
	}

	existingConfigMap := &corev1.ConfigMap{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      configMap.Name,
		Namespace: configMap.Namespace,
	}, existingConfigMap)
	if err != nil && !errors.IsNotFound(err) {
		return nil, fmt.Errorf("getting ConfigMap: %w", err)
	}
	if errors.IsNotFound(err) {
		if err := r.Create(ctx, configMap); err != nil {
			return nil, fmt.Errorf("creating ConfigMap: %w", err)
		}
		return configMap, nil
	}
	existingConfigMap.Data = configMap.Data
	if err := r.Update(ctx, existingConfigMap); err != nil {
		return nil, fmt.Errorf("updating ConfigMap: %w", err)
	}
	return existingConfigMap, nil
}

type metaTypeObject interface {
	metav1.Type
	metav1.Object
}

func objectKey(obj metaTypeObject) string {
	return fmt.Sprintf("%s:%s %s/%s",
		obj.GetAPIVersion(),
		obj.GetKind(),
		obj.GetNamespace(),
		obj.GetName())
}

func mapFromMapTemplate(templateMap map[string]string, instance *corev1alpha1.InterfaceInstance) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range templateMap {
		value, err := stringFromTemplate(v, instance)
		if err != nil {
			return nil, fmt.Errorf("templating value for key [%s]: %w", k, err)
		}
		out[k] = value
	}
	return out, nil
}

func stringFromTemplate(templateString string, instance *corev1alpha1.InterfaceInstance) (string, error) {
	t, err := template.New("name").Parse(templateString)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, instance); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}
	return strings.TrimSpace(buf.String()), nil
}

func objectFromTemplate(
	instance *corev1alpha1.InterfaceInstance,
	mappedObject corev1alpha1.MappedObject,
	scheme *runtime.Scheme,
) (*unstructured.Unstructured, error) {
	t, err := template.New("obj").Parse(mappedObject.Template)
	if err != nil {
		return nil, fmt.Errorf("parsing template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, instance); err != nil {
		return nil, fmt.Errorf("executing template: %w", err)
	}
	obj := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(buf.Bytes(), obj); err != nil {
		return nil, fmt.Errorf("unmarshal YAML: %w", err)
	}
	obj.SetNamespace(instance.Namespace)
	if err := controllerutil.SetControllerReference(instance, obj, scheme); err != nil {
		return nil, fmt.Errorf("set controller reference: %w", err)
	}
	return obj, nil
}

func (r *InterfaceInstanceReconciler) reconcileMappedObject(
	ctx context.Context,
	instance *corev1alpha1.InterfaceInstance,
	mappedObject corev1alpha1.MappedObject,
) (ready bool, key string, err error) {
	obj, err := objectFromTemplate(instance, mappedObject, r.Scheme)
	if err != nil {
		return false, "", fmt.Errorf("creating object from template: %w", err)
	}
	if err := r.DynamicWatcher.Watch(instance, obj); err != nil {
		return false, "", fmt.Errorf("adding watch: %w", err)
	}

	// reconcile
	existingObj := &unstructured.Unstructured{}
	existingObj.SetGroupVersionKind(obj.GroupVersionKind())
	err = r.Get(ctx, types.NamespacedName{
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
	}, existingObj)
	if err != nil && !errors.IsNotFound(err) {
		return false, "", fmt.Errorf("getting object: %w", err)
	}
	if errors.IsNotFound(err) {
		if err := r.Create(ctx, obj); err != nil {
			return false, "", fmt.Errorf("creating object: %w", err)
		}
		return false, objectKey(obj), nil
	}

	// Make sure all specified fields are set
	overrideFields(obj, existingObj)
	if err := r.Update(ctx, existingObj); err != nil {
		return false, "", fmt.Errorf("updating object: %w", err)
	}

	// check readiness
	if mappedObject.ReadinessProbe != nil {
		jsonPath := jsonpath.New("readiness")
		if err := jsonPath.Parse(mappedObject.ReadinessProbe.JSONPath); err != nil {
			return false, "", fmt.Errorf("parsing JSONPath %q: %w", mappedObject.ReadinessProbe.JSONPath, err)
		}

		var buf bytes.Buffer
		jsonPath.AllowMissingKeys(true)
		if err := jsonPath.Execute(&buf, existingObj.Object); err != nil {
			return false, "", fmt.Errorf("searching JSONPath: %w", err)
		}

		r, err := regexp.Compile(mappedObject.ReadinessProbe.RegexTest)
		if err != nil {
			return false, "", fmt.Errorf("compiling: %w", err)
		}
		if r.Match(buf.Bytes()) {
			// ready!
			return true, objectKey(obj), nil
		}
		return false, objectKey(obj), nil
	}

	// ready by default
	return true, objectKey(obj), nil
}

func (r *InterfaceInstanceReconciler) staticProvisioner(
	ctx context.Context,
	instance *corev1alpha1.InterfaceInstance,
	provisioner *corev1alpha1.InterfaceProvisioner,
) error {
	instance.Spec.Outputs = provisioner.Static.Outputs
	if err := r.Update(ctx, instance); err != nil {
		return fmt.Errorf("updating instance: %w", err)
	}

	instance.Status.SetCondition(corev1alpha1.InterfaceInstanceCondition{
		Type:    corev1alpha1.InterfaceInstanceAvailable,
		Status:  corev1alpha1.ConditionTrue,
		Reason:  "AllComponentsReady",
		Message: "Static provisioned instances don't need setup.",
	})
	instance.Status.SetCondition(corev1alpha1.InterfaceInstanceCondition{
		Type:    corev1alpha1.InterfaceInstanceReady,
		Status:  corev1alpha1.ConditionTrue,
		Reason:  "AllComponentsReady",
		Message: "Static provisioned instances are always considered Ready.",
	})
	instance.Status.ObservedGeneration = instance.Generation
	if err := r.Status().Update(ctx, instance); err != nil {
		return fmt.Errorf("updating instance status: %w", err)
	}
	return nil
}

var instanceLabelPrefixMapper = &handler.EnqueueRequestsFromMapFunc{
	ToRequests: handler.ToRequestsFunc(func(obj handler.MapObject) []reconcile.Request {
		var (
			requests []reconcile.Request
		)
		for label := range obj.Meta.GetLabels() {
			if !strings.HasPrefix(label, instanceLabelPrefix) {
				continue
			}

			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      label[len(instanceLabelPrefix):],
					Namespace: obj.Meta.GetNamespace(),
				},
			})
		}
		return requests
	}),
}

func (r *InterfaceInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.InterfaceInstance{}).
		Watches(
			r.DynamicWatcher,
			&handler.EnqueueRequestForOwner{
				OwnerType: &corev1alpha1.InterfaceInstance{},
			},
		).
		Watches(
			&source.Kind{Type: &corev1.Secret{}},
			instanceLabelPrefixMapper,
		).
		Watches(
			&source.Kind{Type: &corev1.ConfigMap{}},
			instanceLabelPrefixMapper,
		).
		Watches(
			&source.Kind{Type: &corev1alpha1.InterfaceClaim{}},
			&handler.EnqueueRequestsFromMapFunc{
				ToRequests: handler.ToRequestsFunc(func(obj handler.MapObject) []reconcile.Request {
					instance, ok := obj.Object.(*corev1alpha1.InterfaceClaim)
					if !ok || instance.Spec.Instance == nil {
						return nil
					}

					return []reconcile.Request{
						{
							NamespacedName: types.NamespacedName{
								Name:      instance.Spec.Instance.Name,
								Namespace: instance.Namespace,
							},
						},
					}
				}),
			},
		).
		Complete(r)
}
