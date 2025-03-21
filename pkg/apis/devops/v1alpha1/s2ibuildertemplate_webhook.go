/*
Copyright 2019 The KubeSphere Authors.

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
	"context"
	"fmt"

	"github.com/distribution/reference"
	"github.com/kubesphere/s2ioperator/pkg/errors"
	"github.com/kubesphere/s2ioperator/pkg/util/reflectutils"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var s2ibuildertemplatelog = ctrl.Log.WithName("s2ibuildertemplate-resource")

func (r *S2iBuilderTemplate) SetupWebhookWithManager(mgr ctrl.Manager) error {
	kclient = mgr.GetClient()
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// +kubebuilder:webhook:verbs=create;update,path=/validate-devops-kubesphere-io-v1alpha1-s2ibuildertemplate,mutating=false,failurePolicy=fail,groups=devops.kubesphere.io,resources=s2ibuildertemplates,versions=v1alpha1,name=s2ibuildertemplate.kb.io

var _ webhook.CustomValidator = &S2iBuilderTemplate{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *S2iBuilderTemplate) ValidateCreate(ctx context.Context, obj runtime.Object) (warnings admission.Warnings, err error) {
	s2ibuildertemplatelog.Info("validate create", "name", r.Name)

	if len(r.Spec.ContainerInfo) == 0 {
		return nil, errors.NewFieldRequired("baseImages")
	}

	if r.Spec.DefaultBaseImage == "" {
		return nil, errors.NewFieldRequired("defaultBaseImage")
	}
	var builderImages []string
	for _, ImageInfo := range r.Spec.ContainerInfo {
		builderImages = append(builderImages, ImageInfo.BuilderImage)
	}
	if !reflectutils.Contains(r.Spec.DefaultBaseImage, builderImages) {
		return nil, errors.NewFieldInvalidValueWithReason("defaultBaseImage",
			fmt.Sprintf("defaultBaseImage [%s] should in [%v]", r.Spec.DefaultBaseImage, builderImages))
	}

	for _, ImageInfo := range r.Spec.ContainerInfo {
		if err := validateDockerReference(ImageInfo.BuilderImage); err != nil {
			return nil, errors.NewFieldInvalidValueWithReason("builderImage", err.Error())
		}
	}
	if err := validateDockerReference(r.Spec.DefaultBaseImage); err != nil {
		return nil, errors.NewFieldInvalidValueWithReason("defaultBaseImage", err.Error())
	}
	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *S2iBuilderTemplate) ValidateUpdate(ctx context.Context, oldObj runtime.Object, newObj runtime.Object) (warnings admission.Warnings, err error) {
	s2ibuildertemplatelog.Info("validate update", "name", r.Name)

	if len(r.Spec.ContainerInfo) == 0 {
		return nil, errors.NewFieldRequired("baseImages")
	}

	if r.Spec.DefaultBaseImage == "" {
		return nil, errors.NewFieldRequired("defaultBaseImage")
	}
	var builderImages []string
	for _, ImageInfo := range r.Spec.ContainerInfo {
		builderImages = append(builderImages, ImageInfo.BuilderImage)
	}
	if !reflectutils.Contains(r.Spec.DefaultBaseImage, builderImages) {
		return nil, errors.NewFieldInvalidValueWithReason("defaultBaseImage",
			fmt.Sprintf("defaultBaseImage [%s] should in [%v]", r.Spec.DefaultBaseImage, builderImages))
	}

	for _, ImageInfo := range r.Spec.ContainerInfo {
		if err := validateDockerReference(ImageInfo.BuilderImage); err != nil {
			return nil, errors.NewFieldInvalidValueWithReason("builderImage", err.Error())
		}
	}
	if err := validateDockerReference(r.Spec.DefaultBaseImage); err != nil {
		return nil, errors.NewFieldInvalidValueWithReason("defaultBaseImage", err.Error())
	}
	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *S2iBuilderTemplate) ValidateDelete(ctx context.Context, obj runtime.Object) (warnings admission.Warnings, err error) {
	s2ibuildertemplatelog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil, nil
}

func validateDockerReference(ref string) error {
	_, err := reference.Parse(ref)
	return err
}
