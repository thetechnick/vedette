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
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/go-logr/logr"
	corev1alpha1 "github.com/thetechnick/vedette/api/v1alpha1"
)

var (
	_ WatcherInterface = (*DynamicWatcher)(nil)
)

type WatcherInterface interface {
	source.Source
	Watch(owner *corev1alpha1.InterfaceInstance, obj runtime.Object) error
	Free(owner *corev1alpha1.InterfaceInstance) error
}

type NamespacedGKV struct {
	schema.GroupVersionKind
	Namespace string
}

type DynamicWatcher struct {
	log        logr.Logger
	scheme     *runtime.Scheme
	restMapper meta.RESTMapper
	client     dynamic.Interface

	handler    handler.EventHandler
	queue      workqueue.RateLimitingInterface
	predicates []predicate.Predicate

	mu                 sync.Mutex
	informers          map[NamespacedGKV]chan<- struct{}
	informerReferences map[NamespacedGKV]map[types.UID]struct{}
}

func NewDynamicWatcher(
	log logr.Logger,
	scheme *runtime.Scheme,
	restMapper meta.RESTMapper,
	client dynamic.Interface,
) *DynamicWatcher {
	return &DynamicWatcher{
		log:        log,
		scheme:     scheme,
		restMapper: restMapper,
		client:     client,

		informers:          map[NamespacedGKV]chan<- struct{}{},
		informerReferences: map[NamespacedGKV]map[types.UID]struct{}{},
	}
}

func (dw *DynamicWatcher) Start(handler handler.EventHandler, queue workqueue.RateLimitingInterface, predicates ...predicate.Predicate) error {
	dw.handler = handler
	dw.queue = queue
	dw.predicates = predicates
	return nil
}

// watch the given object type and associate the watch with the given owner.
func (dw *DynamicWatcher) Watch(owner *corev1alpha1.InterfaceInstance, obj runtime.Object) error {
	dw.mu.Lock()
	defer dw.mu.Unlock()

	gvk, err := apiutil.GVKForObject(obj, dw.scheme)
	if err != nil {
		return fmt.Errorf("get GVK for object: %w", err)
	}
	ngvk := NamespacedGKV{
		Namespace:        owner.Namespace,
		GroupVersionKind: gvk,
	}

	// Check if informer already registered
	if _, ok := dw.informers[ngvk]; !ok {
		dw.informerReferences[ngvk] = map[types.UID]struct{}{}
	}
	dw.informerReferences[ngvk][owner.UID] = struct{}{}
	if _, ok := dw.informers[ngvk]; ok {
		dw.log.Info("reusing existing watcher",
			"kind", gvk.Kind, "group", gvk.Group, "namespace", owner.Namespace)
		return nil
	}
	informerStopChannel := make(chan struct{})
	dw.informers[ngvk] = informerStopChannel
	dw.log.Info("adding new watcher",
		"kind", gvk.Kind, "group", gvk.Group, "namespace", owner.Namespace)

	// Build client
	restMapping, err := dw.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("unable to map object to rest endpoint: %w", err)
	}
	client := dw.client.Resource(restMapping.Resource)

	informer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return client.Namespace(owner.Namespace).List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return client.Namespace(owner.Namespace).Watch(options)
			},
		},
		obj, 10*time.Hour, nil)
	s := source.Informer{
		Informer: informer,
	}

	if err := s.Start(dw.handler, dw.queue, dw.predicates...); err != nil {
		return err
	}
	go informer.Run(informerStopChannel)
	return nil
}

// free all watches associated with the given owner.
func (dw *DynamicWatcher) Free(owner *corev1alpha1.InterfaceInstance) error {
	dw.mu.Lock()
	defer dw.mu.Unlock()

	for gvk, refs := range dw.informerReferences {
		if _, ok := refs[owner.UID]; ok {
			delete(refs, owner.UID)

			if len(refs) == 0 {
				close(dw.informers[gvk])
				delete(dw.informers, gvk)
				dw.log.Info("releasing watcher",
					"kind", gvk.Kind, "group", gvk.Group, "namespace", owner.Namespace)
			}
		}
	}
	return nil
}
