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

	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	corev1alpha1 "github.com/thetechnick/vedette/api/v1alpha1"
)

var testScheme = runtime.NewScheme()

func init() {
	utilruntime.Must(corev1.AddToScheme(testScheme))
	utilruntime.Must(corev1alpha1.AddToScheme(testScheme))
}

type DynamicWatcherMock struct {
	mock.Mock
}

func NewDynamicWatcherMock() *DynamicWatcherMock {
	return &DynamicWatcherMock{}
}

func (c *DynamicWatcherMock) Free(owner *corev1alpha1.InterfaceInstance) error {
	args := c.Called(owner)
	return args.Error(0)
}

func (c *DynamicWatcherMock) Watch(owner *corev1alpha1.InterfaceInstance, obj runtime.Object) error {
	args := c.Called(owner, obj)
	return args.Error(0)
}

func (c *DynamicWatcherMock) Start(
	handler handler.EventHandler,
	queue workqueue.RateLimitingInterface,
	predicates ...predicate.Predicate,
) error {
	args := c.Called(handler, queue, predicates)
	return args.Error(0)
}

// ClientMock is a mock for the controller-runtime dynamic client interface.
type ClientMock struct {
	mock.Mock

	StatusMock *StatusClientMock
}

var _ client.Client = &ClientMock{}

func NewClient() *ClientMock {
	return &ClientMock{
		StatusMock: &StatusClientMock{},
	}
}

// StatusClientMock interface

func (c *ClientMock) Status() client.StatusWriter {
	return c.StatusMock
}

// Reader interface

func (c *ClientMock) Get(ctx context.Context, key types.NamespacedName, obj runtime.Object) error {
	args := c.Called(ctx, key, obj)
	return args.Error(0)
}

func (c *ClientMock) List(ctx context.Context, list runtime.Object, opts ...client.ListOption) error {
	args := c.Called(ctx, list, opts)
	return args.Error(0)
}

// Writer interface

func (c *ClientMock) Create(ctx context.Context, obj runtime.Object, opts ...client.CreateOption) error {
	args := c.Called(ctx, obj, opts)
	return args.Error(0)
}

func (c *ClientMock) Delete(ctx context.Context, obj runtime.Object, opts ...client.DeleteOption) error {
	args := c.Called(ctx, obj, opts)
	return args.Error(0)
}

func (c *ClientMock) Update(ctx context.Context, obj runtime.Object, opts ...client.UpdateOption) error {
	args := c.Called(ctx, obj, opts)
	return args.Error(0)
}

func (c *ClientMock) Patch(ctx context.Context, obj runtime.Object, patch client.Patch, opts ...client.PatchOption) error {
	args := c.Called(ctx, obj, patch, opts)
	return args.Error(0)
}

func (c *ClientMock) DeleteAllOf(ctx context.Context, obj runtime.Object, opts ...client.DeleteAllOfOption) error {
	args := c.Called(ctx, obj, opts)
	return args.Error(0)
}

type StatusClientMock struct {
	mock.Mock
}

var _ client.StatusWriter = &StatusClientMock{}

func (c *StatusClientMock) Update(ctx context.Context, obj runtime.Object, opts ...client.UpdateOption) error {
	args := c.Called(ctx, obj, opts)
	return args.Error(0)
}

func (c *StatusClientMock) Patch(ctx context.Context, obj runtime.Object, patch client.Patch, opts ...client.PatchOption) error {
	args := c.Called(ctx, obj, patch, opts)
	return args.Error(0)
}
