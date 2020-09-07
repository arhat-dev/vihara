/*
Copyright 2020 The arhat.dev Authors.

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

// Code generated by client-gen. DO NOT EDIT.

package v1alpha1

import (
	"context"
	"time"

	scheme "arhat.dev/vihara/pkg/apis/vihara/generated/clientset/versioned/scheme"
	v1alpha1 "arhat.dev/vihara/pkg/apis/vihara/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// MaintenanceGetter has a method to return a MaintenanceInterface.
// A group's client should implement this interface.
type MaintenanceGetter interface {
	Maintenance() MaintenanceInterface
}

// MaintenanceInterface has methods to work with Maintenance resources.
type MaintenanceInterface interface {
	Create(ctx context.Context, maintenance *v1alpha1.Maintenance, opts v1.CreateOptions) (*v1alpha1.Maintenance, error)
	Update(ctx context.Context, maintenance *v1alpha1.Maintenance, opts v1.UpdateOptions) (*v1alpha1.Maintenance, error)
	UpdateStatus(ctx context.Context, maintenance *v1alpha1.Maintenance, opts v1.UpdateOptions) (*v1alpha1.Maintenance, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*v1alpha1.Maintenance, error)
	List(ctx context.Context, opts v1.ListOptions) (*v1alpha1.MaintenanceList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.Maintenance, err error)
	MaintenanceExpansion
}

// maintenance implements MaintenanceInterface
type maintenance struct {
	client rest.Interface
}

// newMaintenance returns a Maintenance
func newMaintenance(c *ViharaV1alpha1Client) *maintenance {
	return &maintenance{
		client: c.RESTClient(),
	}
}

// Get takes name of the maintenance, and returns the corresponding maintenance object, and an error if there is any.
func (c *maintenance) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.Maintenance, err error) {
	result = &v1alpha1.Maintenance{}
	err = c.client.Get().
		Resource("maintenance").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of Maintenance that match those selectors.
func (c *maintenance) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.MaintenanceList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1alpha1.MaintenanceList{}
	err = c.client.Get().
		Resource("maintenance").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested maintenance.
func (c *maintenance) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Resource("maintenance").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a maintenance and creates it.  Returns the server's representation of the maintenance, and an error, if there is any.
func (c *maintenance) Create(ctx context.Context, maintenance *v1alpha1.Maintenance, opts v1.CreateOptions) (result *v1alpha1.Maintenance, err error) {
	result = &v1alpha1.Maintenance{}
	err = c.client.Post().
		Resource("maintenance").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(maintenance).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a maintenance and updates it. Returns the server's representation of the maintenance, and an error, if there is any.
func (c *maintenance) Update(ctx context.Context, maintenance *v1alpha1.Maintenance, opts v1.UpdateOptions) (result *v1alpha1.Maintenance, err error) {
	result = &v1alpha1.Maintenance{}
	err = c.client.Put().
		Resource("maintenance").
		Name(maintenance.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(maintenance).
		Do(ctx).
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *maintenance) UpdateStatus(ctx context.Context, maintenance *v1alpha1.Maintenance, opts v1.UpdateOptions) (result *v1alpha1.Maintenance, err error) {
	result = &v1alpha1.Maintenance{}
	err = c.client.Put().
		Resource("maintenance").
		Name(maintenance.Name).
		SubResource("status").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(maintenance).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the maintenance and deletes it. Returns an error if one occurs.
func (c *maintenance) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	return c.client.Delete().
		Resource("maintenance").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *maintenance) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Resource("maintenance").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched maintenance.
func (c *maintenance) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.Maintenance, err error) {
	result = &v1alpha1.Maintenance{}
	err = c.client.Patch(pt).
		Resource("maintenance").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}
