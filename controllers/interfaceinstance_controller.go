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

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1alpha1 "github.com/thetechnick/vedette/api/v1alpha1"
)

// InterfaceInstanceReconciler reconciles a InterfaceInstance object
type InterfaceInstanceReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core.vedette.io,resources=interfaceinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.vedette.io,resources=interfaceinstances/status,verbs=get;update;patch

func (r *InterfaceInstanceReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()
	_ = r.Log.WithValues("interfaceinstance", req.NamespacedName)

	// your logic here

	return ctrl.Result{}, nil
}

func (r *InterfaceInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.InterfaceInstance{}).
		Complete(r)
}
