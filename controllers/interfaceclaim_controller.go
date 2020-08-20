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
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	corev1alpha1 "github.com/thetechnick/vedette/api/v1alpha1"
)

const claimLabel = "vedette.io/claim"

// InterfaceClaimReconciler reconciles a InterfaceClaim object
type InterfaceClaimReconciler struct {
	client.Client
	UncachedClient client.Client
	Log            logr.Logger
	Scheme         *runtime.Scheme
}

// +kubebuilder:rbac:groups=core.vedette.io,resources=interfaceclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.vedette.io,resources=interfaceclaims/status,verbs=get;update;patch

func (r *InterfaceClaimReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("interfaceclaim", req.NamespacedName)

	interfaceClaim := &corev1alpha1.InterfaceClaim{}
	if k8serrors.IsNotFound(r.Get(ctx, req.NamespacedName, interfaceClaim)) {
		return ctrl.Result{}, nil
	}

	// Step 1:
	// Check if already bound
	if stop, err := r.checkAlreadyBound(ctx, log, interfaceClaim); err != nil {
		return ctrl.Result{}, err
	} else if stop {
		return ctrl.Result{}, nil
	}

	// Step 2:
	// Can we find an existing InterfaceInstance and bind to it?
	if stop, err := r.tryToBindInstance(ctx, log, interfaceClaim); err != nil {
		return ctrl.Result{}, err
	} else if stop {
		return ctrl.Result{}, nil
	}

	// Step 3:
	// Can we find an InterfaceProvisioner to delegate to?
	if stop, err := r.tryToProvisionInstance(ctx, log, interfaceClaim); err != nil {
		return ctrl.Result{}, err
	} else if stop {
		return ctrl.Result{}, nil
	}

	// Nothing matched :(
	// Claim remains unbound
	// retry later
	interfaceClaim.Status.SetCondition(corev1alpha1.InterfaceClaimCondition{
		Type:    corev1alpha1.InterfaceClaimBound,
		Status:  corev1alpha1.ConditionFalse,
		Reason:  "NoMatchingInstance",
		Message: "No matching instance found for the given parameters.",
	})
	if err := r.Status().Update(ctx, interfaceClaim); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating InterfaceClaim: %w", err)
	}
	return ctrl.Result{
		RequeueAfter: time.Second * 10,
	}, nil
}

func (r *InterfaceClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	claimLabelHandler := &handler.EnqueueRequestsFromMapFunc{
		ToRequests: handler.ToRequestsFunc(func(obj handler.MapObject) []reconcile.Request {
			claimName, ok := obj.Meta.GetLabels()[claimLabel]
			if !ok {
				return nil
			}
			return []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      claimName,
						Namespace: obj.Meta.GetNamespace(),
					},
				},
			}
		}),
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.InterfaceClaim{}).
		Watches(
			&source.Kind{Type: &corev1.Secret{}},
			claimLabelHandler,
		).
		Watches(
			&source.Kind{Type: &corev1.ConfigMap{}},
			claimLabelHandler,
		).
		Watches(
			&source.Kind{Type: &corev1alpha1.InterfaceInstance{}},
			&handler.EnqueueRequestsFromMapFunc{
				ToRequests: handler.ToRequestsFunc(func(obj handler.MapObject) []reconcile.Request {
					instance, ok := obj.Object.(*corev1alpha1.InterfaceInstance)
					if !ok || instance.Spec.Claim == nil {
						return nil
					}

					return []reconcile.Request{
						{
							NamespacedName: types.NamespacedName{
								Name:      instance.Spec.Claim.Name,
								Namespace: instance.Namespace,
							},
						},
					}
				}),
			},
		).
		Complete(r)
}

func (r *InterfaceClaimReconciler) checkAlreadyBound(
	ctx context.Context,
	log logr.Logger,
	claim *corev1alpha1.InterfaceClaim,
) (stop bool, err error) {
	// Check Bound Reference
	if claim.Spec.Instance == nil {
		return false, nil
	}

	instance := &corev1alpha1.InterfaceInstance{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      claim.Spec.Instance.Name,
		Namespace: claim.Namespace,
	}, instance)
	if k8serrors.IsNotFound(err) {
		// Reference lost
		claim.Status.SetCondition(corev1alpha1.InterfaceClaimCondition{
			Type:    corev1alpha1.InterfaceClaimLost,
			Status:  corev1alpha1.ConditionTrue,
			Reason:  "InstanceLost",
			Message: "Bound instance can no longer be found.",
		})
		if err = r.Status().Update(ctx, claim); err != nil {
			return false, fmt.Errorf("updating claim status: %w", err)
		}
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("getting Instance: %w", err)
	}

	// Reconcile Secrets and ConfigMaps for consumers
	log.Info("reconciling Secrets and ConfigMaps")
	existingSecretList := &corev1.SecretList{}
	if err := r.List(ctx, existingSecretList, client.MatchingLabels{
		claimLabel: claim.Name,
	}); err != nil {
		return false, fmt.Errorf("listing existing Secrets: %w", err)
	}

	existingConfigMapList := &corev1.ConfigMapList{}
	if err := r.List(ctx, existingConfigMapList, client.MatchingLabels{
		claimLabel: claim.Name,
	}); err != nil {
		return false, fmt.Errorf("listing existing ConfigMaps: %w", err)
	}

	if err := r.reconcileInputs(
		ctx, claim, instance, existingSecretList.Items, existingConfigMapList.Items); err != nil {
		return false, fmt.Errorf("reconciling Secrets and ConfigMaps: %w", err)
	}

	// Everything is alright!
	claim.Status.SetCondition(corev1alpha1.InterfaceClaimCondition{
		Type:    corev1alpha1.InterfaceClaimLost,
		Status:  corev1alpha1.ConditionFalse,
		Reason:  "InstanceFound",
		Message: "Bound instance can be found.",
	})
	claim.Status.SetCondition(corev1alpha1.InterfaceClaimCondition{
		Type:    corev1alpha1.InterfaceClaimBound,
		Status:  corev1alpha1.ConditionTrue,
		Reason:  "Bound",
		Message: "Bound to instance.",
	})
	if err := r.Status().Update(ctx, claim); err != nil {
		return false, fmt.Errorf("updating InterfaceClaim Status: %w", err)
	}

	return true, nil
}

func (r *InterfaceClaimReconciler) reconcileInputs(
	ctx context.Context,
	claim *corev1alpha1.InterfaceClaim,
	instance *corev1alpha1.InterfaceInstance,
	existingSecretList []corev1.Secret,
	existingConfigMapList []corev1.ConfigMap,
) error {
	availableOutputs := map[string]corev1alpha1.OutputReference{}
	for _, output := range instance.Spec.Outputs {
		availableOutputs[output.Name] = output
	}

	// list existing Secrets of this Claim
	existingSecrets := map[string]corev1.Secret{}
	for _, secret := range existingSecretList {
		existingSecrets[secret.Name] = secret
	}

	// list existing ConfigMaps of this Claim
	existingConfigMaps := map[string]corev1.ConfigMap{}
	for _, configMap := range existingConfigMapList {
		existingConfigMaps[configMap.Name] = configMap
	}

	// reconcile
	for _, input := range claim.Spec.Inputs {
		output, ok := availableOutputs[input.Name]
		if !ok {
			// log warning
			continue
		}

		var (
			binaryData map[string][]byte
			stringData map[string]string
		)
		switch {
		case output.ConfigMapRef != nil:
			var err error
			binaryData, stringData, err = r.dataFromConfigMap(ctx, output.ConfigMapRef.Name, claim.Namespace)
			if k8serrors.IsNotFound(errors.Unwrap(err)) {
				// TODO: log error
				continue
			}

			if err != nil {
				return fmt.Errorf("getting output %s from configmap: %w", output.Name, err)
			}

		case output.SecretRef != nil:
			var err error
			binaryData, stringData, err = r.dataFromSecret(ctx, output.SecretRef.Name, claim.Namespace)
			if k8serrors.IsNotFound(errors.Unwrap(err)) {
				// TODO: log error
				continue
			}

			if err != nil {
				return fmt.Errorf("getting output %s from secret: %w", output.Name, err)
			}
		}

		switch {
		case input.ConfigMapRef != nil:
			desiredConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      input.ConfigMapRef.Name,
					Namespace: claim.Namespace,
					Labels: map[string]string{
						claimLabel: claim.Name,
					},
				},
				Data:       stringData,
				BinaryData: binaryData,
			}
			if err := controllerutil.SetControllerReference(claim, desiredConfigMap, r.Scheme); err != nil {
				return fmt.Errorf("set controller reference: %w", err)
			}
			existingConfigMap, ok := existingConfigMaps[desiredConfigMap.Name]
			delete(existingConfigMaps, desiredConfigMap.Name)
			if ok {
				existingConfigMap.Data = desiredConfigMap.Data
				existingConfigMap.BinaryData = desiredConfigMap.BinaryData
				if err := r.Update(ctx, &existingConfigMap); err != nil {
					return fmt.Errorf("updating Secret: %w", err)
				}
				continue
			}

			if err := r.Create(ctx, desiredConfigMap); err != nil {
				return fmt.Errorf("creating Secret: %w", err)
			}

		case input.SecretRef != nil:
			for k, v := range stringData {
				binaryData[k] = []byte(v)
			}

			desiredSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      input.SecretRef.Name,
					Namespace: claim.Namespace,
					Labels: map[string]string{
						claimLabel: claim.Name,
					},
				},
				Data: binaryData,
			}
			if err := controllerutil.SetControllerReference(claim, desiredSecret, r.Scheme); err != nil {
				return fmt.Errorf("set controller reference: %w", err)
			}
			existingSecret, ok := existingSecrets[desiredSecret.Name]
			delete(existingSecrets, desiredSecret.Name)
			if ok {
				existingSecret.Data = desiredSecret.Data
				if err := r.Update(ctx, &existingSecret); err != nil {
					return fmt.Errorf("updating Secret: %w", err)
				}
				continue
			}

			if err := r.Create(ctx, desiredSecret); err != nil {
				return fmt.Errorf("creating Secret: %w", err)
			}
		}

	}

	// delete extra Secrets
	for _, existingSecret := range existingSecrets {
		if err := r.Delete(ctx, &existingSecret); err != nil {
			return fmt.Errorf("deleting Secret: %w", err)
		}
	}
	// delete extra ConfigMaps
	for _, existingConfigMap := range existingConfigMaps {
		if err := r.Delete(ctx, &existingConfigMap); err != nil {
			return fmt.Errorf("deleting ConfigMap: %w", err)
		}
	}
	return nil
}

func (r *InterfaceClaimReconciler) dataFromSecret(
	ctx context.Context,
	name, namespace string,
) (map[string][]byte, map[string]string, error) {
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}, secret); err != nil {
		return nil, nil, err
	}
	return secret.Data, nil, nil
}

func (r *InterfaceClaimReconciler) dataFromConfigMap(
	ctx context.Context,
	name, namespace string,
) (map[string][]byte, map[string]string, error) {
	configMap := &corev1.ConfigMap{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}, configMap); err != nil {
		return nil, nil, err
	}
	return configMap.BinaryData, configMap.Data, nil
}

type byLeastCapacity []corev1alpha1.InterfaceInstance

func (b byLeastCapacity) Len() int      { return len(b) }
func (b byLeastCapacity) Swap(i, j int) { b[i], b[j] = b[j], b[i] }
func (b byLeastCapacity) Less(i, j int) bool {
	var more, less int
	for k, v := range b[i].Spec.Capacity {
		jv, ok := b[j].Spec.Capacity[k]
		if !ok {
			continue
		}
		if v.Value() < jv.Value() {
			// is less than
			less++
		} else if v.Value() > jv.Value() {
			// is more than
			more++
		}
	}
	// i is less than j
	// when more capacity values are less than j's
	fmt.Printf(
		"%s is less than %s: %t (L: %d, M: %d)\n",
		b[i].Name, b[j].Name, less >= more, less, more)
	return less >= more
}

// Try to find a matching InterfaceInstance and bind to it.
func (r *InterfaceClaimReconciler) tryToBindInstance(
	ctx context.Context,
	log logr.Logger,
	claim *corev1alpha1.InterfaceClaim,
) (stop bool, err error) {
	instanceSelector, err := metav1.LabelSelectorAsSelector(&claim.Spec.Selector)
	if err != nil {
		// should have been covered by validation
		return false, fmt.Errorf("parsing LabelSelector as Selector: %w", err)
	}

	if claim.Spec.Instance == nil {
		// Find a matching InterfaceInstance to bind to
		interfaceInstanceList := &corev1alpha1.InterfaceInstanceList{}
		if err := r.List(
			ctx,
			interfaceInstanceList,
			client.InNamespace(claim.Namespace),
			client.MatchingLabelsSelector{Selector: instanceSelector},
		); err != nil {
			return false, fmt.Errorf("listing InterfaceInstances: %w", err)
		}
		for _, interfaceInstance := range interfaceInstanceList.Items {
			if claimMatchesInstance(log, claim, &interfaceInstance) {
				claim.Spec.Instance = &corev1alpha1.ObjectReference{
					Name: interfaceInstance.Name,
				}
				break
			}
		}
	}

	if claim.Spec.Instance == nil {
		// no matching instance found
		// claim remains unbound
		return false, nil
	}

	// Bind to an instance
	if err = r.Update(ctx, claim); err != nil {
		return false, fmt.Errorf("updating Claim: %w", err)
	}

	instance := &corev1alpha1.InterfaceInstance{}
	if err = r.Get(ctx, types.NamespacedName{
		Name:      claim.Spec.Instance.Name,
		Namespace: claim.Namespace,
	}, instance); err != nil {
		return false, fmt.Errorf("getting supposed-to-be bound Instance: %w", err)
	}
	if instance.Spec.Claim != nil &&
		instance.Spec.Claim.Name != claim.Name {
		// oh-no! This is not supposed to happen.
		return true, fmt.Errorf(
			"tried to bind to instance %s already bound to claim %s: %w", instance.Name, instance.Spec.Claim.Name, err)
	}
	instance.Spec.Claim = &corev1alpha1.ObjectReference{
		Name: claim.Name,
	}
	if err = r.Update(ctx, instance); err != nil {
		return false, fmt.Errorf("updating Instance: %w", err)
	}
	return
}

// Try to find a suitable InterfaceProvisioner, create a new Instance with it and bind to it.
func (r *InterfaceClaimReconciler) tryToProvisionInstance(
	ctx context.Context,
	log logr.Logger,
	claim *corev1alpha1.InterfaceClaim,
) (stop bool, err error) {
	provisionerList := &corev1alpha1.InterfaceProvisionerList{}
	if err := r.List(ctx, provisionerList, client.InNamespace(claim.Namespace)); err != nil {
		return false, fmt.Errorf("listing Provisioners: %w", err)
	}
	var provisioner *corev1alpha1.InterfaceProvisioner
	for _, interfaceProvisioner := range provisionerList.Items {
		if claimMatchesProvisioner(log, claim, &interfaceProvisioner) {
			provisioner = &interfaceProvisioner
			break
		}
	}
	if provisioner == nil {
		// no provisioner found
		return false, nil
	}

	// Is there already an instance for this claim?
	// We need to use an uncached client, or we might run into a race condition.
	// This race condition can be triggered when we already created a new Instance below,
	// but our local cache was not yet updated. Resulting in multiple Instaces beeing created.
	instanceList := &corev1alpha1.InterfaceInstanceList{}
	if err = r.UncachedClient.List(
		ctx, instanceList,
		client.InNamespace(claim.Namespace), client.MatchingLabels{
			claimLabel: claim.Name,
		}); err != nil {
		return false, fmt.Errorf("listing existing Instances: %w", err)
	}
	var existingInstance *corev1alpha1.InterfaceInstance
	if len(instanceList.Items) > 1 {
		log.Error(fmt.Errorf("multiple Instances bound to same claim"), "defaulting to first found instance")
	}
	if len(instanceList.Items) == 1 {
		// Instance does al
		existingInstance = &instanceList.Items[0]
	}

	if existingInstance == nil {
		// Create a new one
		existingInstance = &corev1alpha1.InterfaceInstance{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: claim.Name + "-",
				Namespace:    claim.Namespace,
				Labels: map[string]string{
					// field selectors are not supported, so we need to setup another kind of back-ref
					// to find this instance again, when the Update on the Claim instance fails below.
					// Not doing this would result in this controller creating multiple Instances.
					claimLabel: claim.Name,
				},
			},
			Spec: corev1alpha1.InterfaceInstanceSpec{
				ProvisionerName: provisioner.Name,
				Capabilities:    provisioner.Capabilities,
				Claim: &corev1alpha1.ObjectReference{
					Name: claim.Name,
				},
			},
		}
		if err = r.Create(ctx, existingInstance); err != nil {
			return false, fmt.Errorf("creating new Instance for Provisioner: %w", err)
		}
	}

	claim.Spec.Instance = &corev1alpha1.ObjectReference{
		Name: existingInstance.Name,
	}
	if err = r.Update(ctx, claim); err != nil {
		return false, fmt.Errorf("updating Claim: %w", err)
	}

	// InstanceClaim will be bound / status is updated when the reconciler is called for a 2nd time.
	return true, nil
}

func claimMatchesInstance(
	log logr.Logger,
	claim *corev1alpha1.InterfaceClaim,
	instance *corev1alpha1.InterfaceInstance,
) bool {
	// Check instance status
	bound := instance.Status.GetCondition(corev1alpha1.InterfaceInstanceBound)
	if bound.Status == corev1alpha1.ConditionTrue ||
		instance.Spec.Claim == nil ||
		instance.Spec.Claim.Name != "" {
		// already bound
		return false
	}
	if bound.Status == corev1alpha1.ConditionFalse &&
		bound.Reason == "Released" {
		// has been released from a previous claim
		// Released instances need to be bound to a new claim explicitly
		return false
	}

	if instance.Status.GetCondition(corev1alpha1.InterfaceInstanceAvailable).Status != corev1alpha1.ConditionTrue {
		// unavailable
		return false
	}

	// Check capabilities
	claimCapabilities := sets.NewString(claim.Spec.Capabilities...)
	instanceCapabilities := sets.NewString(instance.Spec.Capabilities...)
	if !instanceCapabilities.HasAll(claimCapabilities.UnsortedList()...) {
		// Instance does not provide needed capabilities.
		return false
	}

	// Check capacities
	for k, v := range claim.Spec.Capacity {
		instanceV, ok := instance.Spec.Capacity[k]
		if !ok {
			// Instance does not specify capacity.
			return false
		}
		if instanceV.Value() < v.Value() {
			// Instance does not provide needed capacity.
			return false
		}
	}
	return true
}

func claimMatchesProvisioner(
	log logr.Logger,
	claim *corev1alpha1.InterfaceClaim,
	provisioner *corev1alpha1.InterfaceProvisioner,
) bool {
	// Check capabilities
	claimCapabilities := sets.NewString(claim.Spec.Capabilities...)
	provisionerCapabilities := sets.NewString(provisioner.Capabilities...)
	if !provisionerCapabilities.HasAll(claimCapabilities.UnsortedList()...) {
		// Instance does not provide needed capabilities.
		return false
	}

	// Check capacities
	for k, v := range claim.Spec.Capacity {
		provisionerV, ok := provisioner.MaxCapacity[k]
		if !ok {
			// Provisioner does not specify capacity.
			return false
		}
		if provisionerV.Value() < v.Value() {
			// Provisioner does not provide needed capacity.
			return false
		}
	}
	return true
}
