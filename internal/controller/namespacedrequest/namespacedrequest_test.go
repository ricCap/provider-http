/*
Copyright 2024 The Crossplane Authors.

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

package namespacedrequest

import (
	"context"
	"testing"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane-contrib/provider-http/apis/namespacedrequest/v1alpha2"
	httpClient "github.com/crossplane-contrib/provider-http/internal/clients/http"
	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	xpv2 "github.com/crossplane/crossplane-runtime/v2/apis/common/v2"
	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/crossplane-runtime/v2/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
)

var (
	errBoom = errors.New("boom")
)

const (
	providerName              = "http-test"
	testNamespacedRequestName = "test-namespaced-request"
	testNamespace             = "testns"
)

var (
	testPostMapping = v1alpha2.Mapping{
		Action: "CREATE",
		Method: "POST",
		Body:   ".payload.body",
		URL:    ".payload.baseUrl",
	}
	testGetMapping = v1alpha2.Mapping{
		Action: "OBSERVE",
		Method: "GET",
		URL:    ".payload.baseUrl",
	}
	testPutMapping = v1alpha2.Mapping{
		Action: "UPDATE",
		Method: "PUT",
		Body:   ".payload.body",
		URL:    ".payload.baseUrl",
	}
	testDeleteMapping = v1alpha2.Mapping{
		Action: "REMOVE",
		Method: "DELETE",
		URL:    ".payload.baseUrl",
	}
	testForProvider = v1alpha2.NamespacedRequestParameters{
		Payload: v1alpha2.Payload{
			Body:    "{\"username\": \"john_doe\", \"email\": \"john.doe@example.com\"}",
			BaseUrl: "https://api.example.com/users",
		},
		Mappings: []v1alpha2.Mapping{
			testPostMapping,
			testGetMapping,
			testPutMapping,
			testDeleteMapping,
		},
	}
)

type httpNamespacedRequestModifier func(request *v1alpha2.NamespacedRequest)

func httpNamespacedRequest(rm ...httpNamespacedRequestModifier) *v1alpha2.NamespacedRequest {
	r := &v1alpha2.NamespacedRequest{
		ObjectMeta: v1.ObjectMeta{
			Name:      testNamespacedRequestName,
			Namespace: testNamespace,
		},
		Spec: v1alpha2.NamespacedRequestSpec{
			ManagedResourceSpec: xpv2.ManagedResourceSpec{
				ProviderConfigReference: &xpv1.ProviderConfigReference{
					Name: providerName,
				},
			},
			ForProvider: testForProvider,
		},
		Status: v1alpha2.NamespacedRequestStatus{},
	}

	for _, m := range rm {
		m(r)
	}

	return r
}

type notHttpNamespacedRequest struct {
	resource.Managed
}

type MockSendRequestFn func(ctx context.Context, method string, url string, body httpClient.Data, headers httpClient.Data, skipTLSVerify bool) (resp httpClient.HttpDetails, err error)

type MockHttpClient struct {
	MockSendRequest MockSendRequestFn
}

func (c *MockHttpClient) SendRequest(ctx context.Context, method string, url string, body httpClient.Data, headers httpClient.Data, skipTLSVerify bool) (resp httpClient.HttpDetails, err error) {
	return c.MockSendRequest(ctx, method, url, body, headers, skipTLSVerify)
}

func Test_httpExternal_Create(t *testing.T) {
	type args struct {
		http      httpClient.Client
		localKube client.Client
		mg        resource.Managed
	}
	type want struct {
		err error
	}

	cases := []struct {
		name string
		args args
		want want
	}{
		{
			name: "NotNamespacedRequestResource",
			args: args{
				mg: notHttpNamespacedRequest{},
			},
			want: want{
				err: errors.New(errNotNamespacedRequest),
			},
		},
		{
			name: "NamespacedRequestFailed",
			args: args{
				http: &MockHttpClient{
					MockSendRequest: func(ctx context.Context, method string, url string, body httpClient.Data, headers httpClient.Data, skipTLSVerify bool) (resp httpClient.HttpDetails, err error) {
						return httpClient.HttpDetails{}, errBoom
					},
				},
				localKube: &test.MockClient{
					MockStatusUpdate: test.NewMockSubResourceUpdateFn(nil),
					MockGet:          test.NewMockGetFn(nil),
				},
				mg: httpNamespacedRequest(),
			},
			want: want{
				err: errors.Wrap(errBoom, errFailedToSendHttpRequest),
			},
		},
		{
			name: "Success",
			args: args{
				http: &MockHttpClient{
					MockSendRequest: func(ctx context.Context, method string, url string, body httpClient.Data, headers httpClient.Data, skipTLSVerify bool) (resp httpClient.HttpDetails, err error) {
						return httpClient.HttpDetails{}, nil
					},
				},
				localKube: &test.MockClient{
					MockStatusUpdate: test.NewMockSubResourceUpdateFn(nil),
					MockCreate:       test.NewMockCreateFn(nil),
					MockGet:          test.NewMockGetFn(nil),
				},
				mg: httpNamespacedRequest(),
			},
			want: want{
				err: nil,
			},
		},
	}
	for _, tc := range cases {
		tc := tc // Create local copies of loop variables

		t.Run(tc.name, func(t *testing.T) {
			e := &external{
				localKube: tc.args.localKube,
				logger:    logging.NewNopLogger(),
				http:      tc.args.http,
			}
			_, gotErr := e.Create(context.Background(), tc.args.mg)
			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Fatalf("e.Create(...): -want error, +got error: %s", diff)
			}
		})
	}
}

func Test_httpExternal_Update(t *testing.T) {
	type args struct {
		http      httpClient.Client
		localKube client.Client
		mg        resource.Managed
	}
	type want struct {
		err error
	}

	cases := []struct {
		name string
		args args
		want want
	}{
		{
			name: "NotNamespacedRequestResource",
			args: args{
				mg: notHttpNamespacedRequest{},
			},
			want: want{
				err: errors.New(errNotNamespacedRequest),
			},
		},
		{
			name: "NamespacedRequestFailed",
			args: args{
				http: &MockHttpClient{
					MockSendRequest: func(ctx context.Context, method string, url string, body httpClient.Data, headers httpClient.Data, skipTLSVerify bool) (resp httpClient.HttpDetails, err error) {
						return httpClient.HttpDetails{}, errBoom
					},
				},
				localKube: &test.MockClient{
					MockStatusUpdate: test.NewMockSubResourceUpdateFn(nil),
					MockGet:          test.NewMockGetFn(nil),
				},
				mg: httpNamespacedRequest(),
			},
			want: want{
				err: errors.Wrap(errBoom, errFailedToSendHttpRequest),
			},
		},
		{
			name: "Success",
			args: args{
				http: &MockHttpClient{
					MockSendRequest: func(ctx context.Context, method string, url string, body httpClient.Data, headers httpClient.Data, skipTLSVerify bool) (resp httpClient.HttpDetails, err error) {
						return httpClient.HttpDetails{}, nil
					},
				},
				localKube: &test.MockClient{
					MockStatusUpdate: test.NewMockSubResourceUpdateFn(nil),
					MockCreate:       test.NewMockCreateFn(nil),
					MockGet:          test.NewMockGetFn(nil),
				},
				mg: httpNamespacedRequest(),
			},
			want: want{
				err: nil,
			},
		},
	}
	for _, tc := range cases {
		tc := tc // Create local copies of loop variables

		t.Run(tc.name, func(t *testing.T) {
			e := &external{
				localKube: tc.args.localKube,
				logger:    logging.NewNopLogger(),
				http:      tc.args.http,
			}
			_, gotErr := e.Update(context.Background(), tc.args.mg)
			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Fatalf("e.Update(...): -want error, +got error: %s", diff)
			}
		})
	}
}

func Test_httpExternal_Delete(t *testing.T) {
	type args struct {
		http      httpClient.Client
		localKube client.Client
		mg        resource.Managed
	}
	type want struct {
		err error
	}

	cases := []struct {
		name string
		args args
		want want
	}{
		{
			name: "NotNamespacedRequestResource",
			args: args{
				mg: notHttpNamespacedRequest{},
			},
			want: want{
				err: errors.New(errNotNamespacedRequest),
			},
		},
		{
			name: "NamespacedRequestFailed",
			args: args{
				http: &MockHttpClient{
					MockSendRequest: func(ctx context.Context, method string, url string, body httpClient.Data, headers httpClient.Data, skipTLSVerify bool) (resp httpClient.HttpDetails, err error) {
						return httpClient.HttpDetails{}, errBoom
					},
				},
				localKube: &test.MockClient{
					MockStatusUpdate: test.NewMockSubResourceUpdateFn(nil),
					MockGet:          test.NewMockGetFn(nil),
				},
				mg: httpNamespacedRequest(),
			},
			want: want{
				err: errors.Wrap(errBoom, errFailedToSendHttpRequest),
			},
		},
		{
			name: "Success",
			args: args{
				http: &MockHttpClient{
					MockSendRequest: func(ctx context.Context, method string, url string, body httpClient.Data, headers httpClient.Data, skipTLSVerify bool) (resp httpClient.HttpDetails, err error) {
						return httpClient.HttpDetails{}, nil
					},
				},
				localKube: &test.MockClient{
					MockStatusUpdate: test.NewMockSubResourceUpdateFn(nil),
					MockCreate:       test.NewMockCreateFn(nil),
					MockGet:          test.NewMockGetFn(nil),
				},
				mg: httpNamespacedRequest(),
			},
			want: want{
				err: nil,
			},
		},
	}
	for _, tc := range cases {
		tc := tc // Create local copies of loop variables

		t.Run(tc.name, func(t *testing.T) {
			e := &external{
				localKube: tc.args.localKube,
				logger:    logging.NewNopLogger(),
				http:      tc.args.http,
			}
			_, gotErr := e.Delete(context.Background(), tc.args.mg)
			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Fatalf("e.Delete(...): -want error, +got error: %s", diff)
			}
		})
	}
}
