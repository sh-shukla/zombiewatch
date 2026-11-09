// Package v1alpha1 contains the API schema for the zombiewatch group.
// +kubebuilder:object:generate=true
// +groupName=zombiewatch.sh-shukla.dev
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion identifies this API group and version.
	GroupVersion = schema.GroupVersion{Group: "zombiewatch.sh-shukla.dev", Version: "v1alpha1"}

	// SchemeBuilder registers our types with a scheme.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
