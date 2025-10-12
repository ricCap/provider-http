/*
Copyright 2022 The Crossplane Authors.

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

package v1alpha2

import (
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/crossplane-contrib/provider-http/apis/common"
	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	xpv2 "github.com/crossplane/crossplane-runtime/v2/apis/common/v2"
)

// NamespacedDisposableRequestParameters are the configurable fields of a NamespacedDisposableRequest.
type NamespacedDisposableRequestParameters struct {
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Field 'forProvider.url' is immutable"
	URL string `json:"url"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Field 'forProvider.method' is immutable"
	Method string `json:"method"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Field 'forProvider.headers' is immutable"
	Headers map[string][]string `json:"headers,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Field 'forProvider.body' is immutable"
	Body string `json:"body,omitempty"`

	// WaitTimeout specifies the maximum time duration for waiting.
	WaitTimeout *metav1.Duration `json:"waitTimeout,omitempty"`

	// RollbackRetriesLimit is max number of attempts to retry HTTP request by sending again the request.
	RollbackRetriesLimit *int32 `json:"rollbackRetriesLimit,omitempty"`

	// InsecureSkipTLSVerify, when set to true, skips TLS certificate checks for the HTTP request
	InsecureSkipTLSVerify bool `json:"insecureSkipTLSVerify,omitempty"`

	// ExpectedResponse is a jq filter expression used to evaluate the HTTP response and determine if it matches the expected criteria.
	// The expression should return a boolean; if true, the response is considered expected.
	// Example: '.body.job_status == "success"'
	ExpectedResponse string `json:"expectedResponse,omitempty"`

	// NextReconcile specifies the duration after which the next reconcile should occur.
	NextReconcile *metav1.Duration `json:"nextReconcile,omitempty"`

	// ShouldLoopInfinitely specifies whether the reconciliation should loop indefinitely.
	ShouldLoopInfinitely bool `json:"shouldLoopInfinitely,omitempty"`

	// SecretInjectionConfig specifies the secrets receiving patches from response data.
	SecretInjectionConfigs []common.SecretInjectionConfig `json:"secretInjectionConfigs,omitempty"`
}

// A NamespacedDisposableRequestSpec defines the desired state of a NamespacedDisposableRequest.
type NamespacedDisposableRequestSpec struct {
	xpv2.ManagedResourceSpec `json:",inline"`
	ForProvider              NamespacedDisposableRequestParameters `json:"forProvider"`
}

type Response struct {
	StatusCode int                 `json:"statusCode,omitempty"`
	Body       string              `json:"body,omitempty"`
	Headers    map[string][]string `json:"headers,omitempty"`
}

type Mapping struct {
	Method  string              `json:"method"`
	Body    string              `json:"body,omitempty"`
	URL     string              `json:"url"`
	Headers map[string][]string `json:"headers,omitempty"`
}

// A NamespacedDisposableRequestStatus represents the observed state of a NamespacedDisposableRequest.
type NamespacedDisposableRequestStatus struct {
	xpv1.ResourceStatus `json:",inline"`
	Response            Response `json:"response,omitempty"`
	Failed              int32    `json:"failed,omitempty"`
	Error               string   `json:"error,omitempty"`
	Synced              bool     `json:"synced,omitempty"`
	RequestDetails      Mapping  `json:"requestDetails,omitempty"`

	// LastReconcileTime records the last time the resource was reconciled.
	LastReconcileTime metav1.Time `json:"lastReconcileTime,omitempty"`
}

// +kubebuilder:object:root=true

// A NamespacedDisposableRequest is a namespaced disposable HTTP request resource.
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="SYNCED",type="string",JSONPath=".status.conditions[?(@.type=='Synced')].status"
// +kubebuilder:printcolumn:name="EXTERNAL-NAME",type="string",JSONPath=".metadata.annotations.crossplane\\.io/external-name"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories={crossplane,managed,http}
// +kubebuilder:storageversion
type NamespacedDisposableRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NamespacedDisposableRequestSpec   `json:"spec"`
	Status NamespacedDisposableRequestStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NamespacedDisposableRequestList contains a list of NamespacedDisposableRequest
type NamespacedDisposableRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NamespacedDisposableRequest `json:"items"`
}

// NamespacedDisposableRequest type metadata.
var (
	NamespacedDisposableRequestKind             = reflect.TypeOf(NamespacedDisposableRequest{}).Name()
	NamespacedDisposableRequestGroupKind        = schema.GroupKind{Group: Group, Kind: NamespacedDisposableRequestKind}.String()
	NamespacedDisposableRequestKindAPIVersion   = NamespacedDisposableRequestKind + "." + SchemeGroupVersion.String()
	NamespacedDisposableRequestGroupVersionKind = SchemeGroupVersion.WithKind(NamespacedDisposableRequestKind)
)

func init() {
	SchemeBuilder.Register(&NamespacedDisposableRequest{}, &NamespacedDisposableRequestList{})
}
