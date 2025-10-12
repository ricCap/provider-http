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

package namespaceddisposablerequest

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/crossplane-contrib/provider-http/apis/namespaceddisposablerequest/v1alpha2"
	httpClient "github.com/crossplane-contrib/provider-http/internal/clients/http"
	"github.com/crossplane-contrib/provider-http/internal/utils"
	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	xpv2 "github.com/crossplane/crossplane-runtime/v2/apis/common/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/crossplane-runtime/v2/pkg/test"
)

var (
	errBoom = errors.New("boom")
)

const (
	providerName                        = "http-test"
	testNamespacedDisposableRequestName = "test-namespaced-request"
	testNamespace                       = "testns"
)

var testHeaders = map[string][]string{
	"fruits":                {"apple", "banana", "orange"},
	"colors":                {"red", "green", "blue"},
	"countries":             {"USA", "UK", "India", "Germany"},
	"programming_languages": {"Go", "Python", "JavaScript"},
}

var testTimeout = &v1.Duration{
	Duration: 5 * time.Minute,
}

const (
	testURL    = "https://example-url"
	testMethod = "GET"
	testBody   = "{\"key1\": \"value1\"}"
)

type httpNamespacedDisposableRequestModifier func(request *v1alpha2.NamespacedDisposableRequest)

func httpNamespacedDisposableRequest(rm ...httpNamespacedDisposableRequestModifier) *v1alpha2.NamespacedDisposableRequest {
	r := &v1alpha2.NamespacedDisposableRequest{
		ObjectMeta: v1.ObjectMeta{
			Name:      testNamespacedDisposableRequestName,
			Namespace: testNamespace,
		},
		Spec: v1alpha2.NamespacedDisposableRequestSpec{
			ManagedResourceSpec: xpv2.ManagedResourceSpec{
				ProviderConfigReference: &xpv1.ProviderConfigReference{
					Name: providerName,
				},
			},
			ForProvider: v1alpha2.NamespacedDisposableRequestParameters{
				URL:         testURL,
				Method:      testMethod,
				Headers:     testHeaders,
				Body:        testBody,
				WaitTimeout: testTimeout,
			},
		},
		Status: v1alpha2.NamespacedDisposableRequestStatus{},
	}

	for _, m := range rm {
		m(r)
	}

	return r
}

type MockSendRequestFn func(ctx context.Context, method string, url string, body httpClient.Data, headers httpClient.Data, skipTLSVerify bool) (resp httpClient.HttpDetails, err error)

type MockHttpClient struct {
	MockSendRequest MockSendRequestFn
}

func (c *MockHttpClient) SendRequest(ctx context.Context, method string, url string, body httpClient.Data, headers httpClient.Data, skipTLSVerify bool) (resp httpClient.HttpDetails, err error) {
	return c.MockSendRequest(ctx, method, url, body, headers, skipTLSVerify)
}

type notHttpNamespacedDisposableRequest struct {
	resource.Managed
}

func Test_httpExternal_Create(t *testing.T) {
	type args struct {
		http      httpClient.Client
		localKube client.Client
		mg        resource.Managed
	}
	type want struct {
		err           error
		failuresIndex int32
	}

	cases := []struct {
		name string
		args args
		want want
	}{
		{
			name: "NotNamespacedDisposableRequestResource",
			args: args{
				mg: notHttpNamespacedDisposableRequest{},
			},
			want: want{
				err: errors.New(errNotNamespacedDisposableRequest),
			},
		},
		{
			name: "NamespacedDisposableRequestFailed",
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
				mg: httpNamespacedDisposableRequest(),
			},
			want: want{
				failuresIndex: 1,
				err:           errors.Wrap(errBoom, errFailedToSendHttpRequest),
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
				mg: httpNamespacedDisposableRequest(),
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
				kube:       tc.args.localKube,
				logger:     logging.NewNopLogger(),
				httpClient: tc.args.http,
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
			name: "NotNamespacedDisposableRequestResource",
			args: args{
				mg: notHttpNamespacedDisposableRequest{},
			},
			want: want{
				err: errors.New(errNotNamespacedDisposableRequest),
			},
		},
		{
			name: "NamespacedDisposableRequestFailed",
			args: args{
				http: &MockHttpClient{
					MockSendRequest: func(ctx context.Context, method string, url string, body, headers httpClient.Data, skipTLSVerify bool) (resp httpClient.HttpDetails, err error) {
						return httpClient.HttpDetails{}, errBoom
					},
				},
				localKube: &test.MockClient{
					MockStatusUpdate: test.NewMockSubResourceUpdateFn(nil),
					MockGet:          test.NewMockGetFn(nil),
				},
				mg: httpNamespacedDisposableRequest(),
			},
			want: want{
				err: errors.Wrap(errBoom, errFailedToSendHttpRequest),
			},
		},
		{
			name: "Success",
			args: args{
				http: &MockHttpClient{
					MockSendRequest: func(ctx context.Context, method string, url string, body, headers httpClient.Data, skipTLSVerify bool) (resp httpClient.HttpDetails, err error) {
						return httpClient.HttpDetails{}, nil
					},
				},
				localKube: &test.MockClient{
					MockStatusUpdate: test.NewMockSubResourceUpdateFn(nil),
					MockCreate:       test.NewMockCreateFn(nil),
					MockGet:          test.NewMockGetFn(nil),
				},
				mg: httpNamespacedDisposableRequest(),
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
				kube:       tc.args.localKube,
				logger:     logging.NewNopLogger(),
				httpClient: tc.args.http}
			_, gotErr := e.Update(context.Background(), tc.args.mg)
			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Fatalf("e.Update(...): -want error, +got error: %s", diff)
			}
		})
	}
}

func Test_deployAction(t *testing.T) {
	type args struct {
		cr        *v1alpha2.NamespacedDisposableRequest
		http      httpClient.Client
		localKube client.Client
	}
	type want struct {
		err           error
		failuresIndex int32
		statusCode    int
	}
	cases := map[string]struct {
		args      args
		want      want
		condition bool
	}{
		"SuccessUpdateStatusRequestFailure": {
			args: args{
				http: &MockHttpClient{
					MockSendRequest: func(ctx context.Context, method string, url string, body, headers httpClient.Data, skipTLSVerify bool) (resp httpClient.HttpDetails, err error) {
						return httpClient.HttpDetails{}, errors.Errorf(utils.ErrInvalidURL, "invalid-url")
					},
				},
				localKube: &test.MockClient{
					MockStatusUpdate: test.NewMockSubResourceUpdateFn(nil),
					MockGet:          test.NewMockGetFn(nil),
				},
				cr: &v1alpha2.NamespacedDisposableRequest{
					Spec: v1alpha2.NamespacedDisposableRequestSpec{
						ForProvider: v1alpha2.NamespacedDisposableRequestParameters{
							URL:     "invalid-url",
							Method:  testMethod,
							Headers: testHeaders,
							Body:    testBody,
						},
					},
					Status: v1alpha2.NamespacedDisposableRequestStatus{},
				},
			},
			want: want{
				err:           errors.Errorf(utils.ErrInvalidURL, "invalid-url"),
				failuresIndex: 1,
			},
		},
		"SuccessUpdateStatusCodeError": {
			args: args{
				http: &MockHttpClient{
					MockSendRequest: func(ctx context.Context, method string, url string, body, headers httpClient.Data, skipTLSVerify bool) (resp httpClient.HttpDetails, err error) {
						return httpClient.HttpDetails{
							HttpResponse: httpClient.HttpResponse{
								StatusCode: 400,
								Body:       testBody,
								Headers:    testHeaders,
							},
						}, nil
					},
				},
				localKube: &test.MockClient{
					MockStatusUpdate: test.NewMockSubResourceUpdateFn(nil),
					MockGet:          test.NewMockGetFn(nil),
				},
				cr: &v1alpha2.NamespacedDisposableRequest{
					Spec: v1alpha2.NamespacedDisposableRequestSpec{
						ForProvider: v1alpha2.NamespacedDisposableRequestParameters{
							URL:     testURL,
							Method:  testMethod,
							Headers: testHeaders,
							Body:    testBody,
						},
					},
					Status: v1alpha2.NamespacedDisposableRequestStatus{},
				},
			},
			want: want{
				err:           errors.Errorf(utils.ErrStatusCode, testMethod, strconv.Itoa(400)),
				failuresIndex: 1,
				statusCode:    400,
			},
			condition: true,
		},
		"SuccessUpdateStatusSuccessfulRequest": {
			args: args{
				http: &MockHttpClient{
					MockSendRequest: func(ctx context.Context, method string, url string, body, headers httpClient.Data, skipTLSVerify bool) (resp httpClient.HttpDetails, err error) {
						return httpClient.HttpDetails{
							HttpResponse: httpClient.HttpResponse{
								StatusCode: 200,
								Body:       testBody,
								Headers:    testHeaders,
							},
						}, nil
					},
				},
				localKube: &test.MockClient{
					MockStatusUpdate: test.NewMockSubResourceUpdateFn(nil),
					MockGet:          test.NewMockGetFn(nil),
				},
				cr: &v1alpha2.NamespacedDisposableRequest{
					Spec: v1alpha2.NamespacedDisposableRequestSpec{
						ForProvider: v1alpha2.NamespacedDisposableRequestParameters{
							URL:     testURL,
							Method:  testMethod,
							Headers: testHeaders,
							Body:    testBody,
						},
					},
					Status: v1alpha2.NamespacedDisposableRequestStatus{},
				},
			},
			want: want{
				err:        nil,
				statusCode: 200,
			},
			condition: true,
		},
	}
	for name, tc := range cases {
		tc := tc // Create local copies of loop variables

		t.Run(name, func(t *testing.T) {
			e := &external{
				kube:       tc.args.localKube,
				logger:     logging.NewNopLogger(),
				httpClient: tc.args.http,
			}

			gotErr := e.deployAction(context.Background(), tc.args.cr)
			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Fatalf("deployAction(...): -want error, +got error: %s", diff)
			}

			if gotErr != nil {
				if diff := cmp.Diff(tc.want.failuresIndex, tc.args.cr.Status.Failed); diff != "" {
					t.Fatalf("deployAction(...): -want Status.Failed, +got Status.Failed: %s", diff)
				}
			}

			if tc.condition {
				if diff := cmp.Diff(tc.args.cr.Spec.ForProvider.Body, tc.args.cr.Status.Response.Body); diff != "" {
					t.Fatalf("deployAction(...): -want Status.Response.Body, +got Status.Response.Body: %s", diff)
				}

				if diff := cmp.Diff(tc.want.statusCode, tc.args.cr.Status.Response.StatusCode); diff != "" {
					t.Fatalf("deployAction(...): -want Status.Response.StatusCode, +got Status.Response.StatusCode: %s", diff)
				}

				if diff := cmp.Diff(tc.args.cr.Spec.ForProvider.Headers, tc.args.cr.Status.Response.Headers); diff != "" {
					t.Fatalf("deployAction(...): -want Status.Response.Headers, +got Status.Response.Headers: %s", diff)
				}

				if tc.args.cr.Status.LastReconcileTime.IsZero() {
					t.Fatalf("deployAction(...): -want Status.LastReconcileTime to not be nil, +got nil")
				}
			}
		})
	}
}
