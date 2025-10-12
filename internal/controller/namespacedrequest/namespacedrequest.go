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
	"time"

	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/v2/pkg/controller"
	"github.com/crossplane/crossplane-runtime/v2/pkg/event"
	"github.com/crossplane/crossplane-runtime/v2/pkg/ratelimiter"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"

	"github.com/crossplane-contrib/provider-http/apis/namespacedrequest/v1alpha2"
	requestv1alpha2 "github.com/crossplane-contrib/provider-http/apis/request/v1alpha2"
	apisv1alpha1 "github.com/crossplane-contrib/provider-http/apis/v1alpha1"
	httpClient "github.com/crossplane-contrib/provider-http/internal/clients/http"
	"github.com/crossplane-contrib/provider-http/internal/controller/request/observe"
	"github.com/crossplane-contrib/provider-http/internal/controller/request/requestgen"
	"github.com/crossplane-contrib/provider-http/internal/controller/request/requestmapping"
	"github.com/crossplane-contrib/provider-http/internal/controller/request/statushandler"
	datapatcher "github.com/crossplane-contrib/provider-http/internal/data-patcher"
	"github.com/crossplane-contrib/provider-http/internal/utils"
)

const (
	errNotNamespacedRequest         = "managed resource is not a NamespacedRequest custom resource"
	errTrackPCUsage                 = "cannot track ProviderConfig usage"
	errNewHttpClient                = "cannot create new Http client"
	errProviderNotRetrieved         = "provider could not be retrieved"
	errFailedToSendHttpRequest      = "something went wrong"
	errFailedToCheckIfUpToDate      = "failed to check if request is up to date"
	errFailedToUpdateStatusFailures = "failed to reset status failures counter"
	errFailedUpdateStatusConditions = "failed updating status conditions"
	errPatchDataToSecret            = "Warning, couldn't patch data from request to secret %s:%s:%s, error: %s"
	errGetLatestVersion             = "failed to get the latest version of the resource"
	errExtractCredentials           = "cannot extract credentials"
	errExpectedResponseCheckType    = "%s.Type should be either DEFAULT, CUSTOM or empty"
)

// Setup adds a controller that reconciles NamespacedRequest managed resources.
func Setup(mgr ctrl.Manager, o controller.Options, timeout time.Duration) error {
	name := managed.ControllerName(v1alpha2.NamespacedRequestGroupKind)

	r := managed.NewReconciler(mgr,
		resource.ManagedKind(v1alpha2.NamespacedRequestGroupVersionKind),
		managed.WithExternalConnecter(&connector{
			logger:          o.Logger,
			kube:            mgr.GetClient(),
			usage:           &usageTracker{resource.NewProviderConfigUsageTracker(mgr.GetClient(), &apisv1alpha1.ProviderConfigUsage{})},
			newHttpClientFn: httpClient.NewClient,
		}),
		managed.WithLogger(o.Logger.WithValues("controller", name)),
		managed.WithPollInterval(o.PollInterval),
		managed.WithTimeout(timeout),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))))

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o.ForControllerRuntime()).
		WithEventFilter(resource.DesiredStateChanged()).
		For(&v1alpha2.NamespacedRequest{}).
		Complete(ratelimiter.NewReconciler(name, r, o.GlobalRateLimiter))
}

// usageTracker wraps the provider config usage tracker to handle interface compatibility
type usageTracker struct {
	tracker *resource.ProviderConfigUsageTracker
}

func (u *usageTracker) Track(ctx context.Context, mg resource.Managed) error {
	// Convert resource.Managed to resource.ModernManaged for the new interface
	if modern, ok := mg.(resource.ModernManaged); ok {
		return u.tracker.Track(ctx, modern)
	}
	return errors.New("resource does not implement ModernManaged")
}

// A connector is expected to produce an ExternalClient when its Connect method
// is called.
type connector struct {
	logger          logging.Logger
	kube            client.Client
	usage           resource.Tracker
	newHttpClientFn func(log logging.Logger, timeout time.Duration, creds string) (httpClient.Client, error)
}

// Connect creates a new external client using the provider config.
func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	cr, ok := mg.(*v1alpha2.NamespacedRequest)
	if !ok {
		return nil, errors.New(errNotNamespacedRequest)
	}

	l := c.logger.WithValues("namespacedRequest", cr.Name)

	if err := c.usage.Track(ctx, mg); err != nil {
		return nil, errors.Wrap(err, errTrackPCUsage)
	}

	pc := &apisv1alpha1.ProviderConfig{}
	n := types.NamespacedName{Name: cr.GetProviderConfigReference().Name}
	if err := c.kube.Get(ctx, n, pc); err != nil {
		return nil, errors.Wrap(err, errProviderNotRetrieved)
	}

	creds := ""
	if pc.Spec.Credentials.Source == xpv1.CredentialsSourceSecret {
		data, err := resource.CommonCredentialExtractor(ctx, pc.Spec.Credentials.Source, c.kube, pc.Spec.Credentials.CommonCredentialSelectors)
		if err != nil {
			return nil, errors.Wrap(err, errExtractCredentials)
		}

		creds = string(data)
	}

	h, err := c.newHttpClientFn(l, utils.WaitTimeout(cr.Spec.ForProvider.WaitTimeout), creds)
	if err != nil {
		return nil, errors.Wrap(err, errNewHttpClient)
	}

	return &external{
		localKube: c.kube,
		logger:    l,
		http:      h,
	}, nil
}

// An ExternalClient observes, then either creates, updates, or deletes an
// external resource to ensure it reflects the managed resource's desired state.
type external struct {
	localKube client.Client
	logger    logging.Logger
	http      httpClient.Client
}

// bridgeToRequest converts a NamespacedRequest to a Request so we can reuse existing submodules
func (c *external) bridgeToRequest(nsr *v1alpha2.NamespacedRequest) *requestv1alpha2.Request {
	return &requestv1alpha2.Request{
		TypeMeta:   nsr.TypeMeta,
		ObjectMeta: nsr.ObjectMeta,
		Spec: requestv1alpha2.RequestSpec{
			ManagedResourceSpec: nsr.Spec.ManagedResourceSpec,
			ForProvider: requestv1alpha2.RequestParameters{
				Mappings:               convertMappings(nsr.Spec.ForProvider.Mappings),
				Payload:                convertPayload(nsr.Spec.ForProvider.Payload),
				Headers:                nsr.Spec.ForProvider.Headers,
				WaitTimeout:            nsr.Spec.ForProvider.WaitTimeout,
				InsecureSkipTLSVerify:  nsr.Spec.ForProvider.InsecureSkipTLSVerify,
				SecretInjectionConfigs: nsr.Spec.ForProvider.SecretInjectionConfigs,
				ExpectedResponseCheck:  convertExpectedResponseCheck(nsr.Spec.ForProvider.ExpectedResponseCheck),
				IsRemovedCheck:         convertExpectedResponseCheck(nsr.Spec.ForProvider.IsRemovedCheck),
			},
		},
		Status: requestv1alpha2.RequestStatus{
			ResourceStatus: nsr.Status.ResourceStatus,
			Response:       convertResponse(nsr.Status.Response),
			Cache:          convertCache(nsr.Status.Cache),
			Failed:         nsr.Status.Failed,
			Error:          nsr.Status.Error,
			RequestDetails: convertMapping(nsr.Status.RequestDetails),
		},
	}
}

// bridgeFromRequest updates the NamespacedRequest from the Request after processing
func (c *external) bridgeFromRequest(nsr *v1alpha2.NamespacedRequest, r *requestv1alpha2.Request) {
	nsr.Status.ResourceStatus = r.Status.ResourceStatus
	nsr.Status.Response = v1alpha2.Response{
		StatusCode: r.Status.Response.StatusCode,
		Body:       r.Status.Response.Body,
		Headers:    r.Status.Response.Headers,
	}
	nsr.Status.Cache = v1alpha2.Cache{
		LastUpdated: r.Status.Cache.LastUpdated,
		Response: v1alpha2.Response{
			StatusCode: r.Status.Cache.Response.StatusCode,
			Body:       r.Status.Cache.Response.Body,
			Headers:    r.Status.Cache.Response.Headers,
		},
	}
	nsr.Status.Failed = r.Status.Failed
	nsr.Status.Error = r.Status.Error
	nsr.Status.RequestDetails = v1alpha2.Mapping{
		Method:  r.Status.RequestDetails.Method,
		Action:  r.Status.RequestDetails.Action,
		Body:    r.Status.RequestDetails.Body,
		URL:     r.Status.RequestDetails.URL,
		Headers: r.Status.RequestDetails.Headers,
	}
}

// Conversion helper functions
func convertMappings(mappings []v1alpha2.Mapping) []requestv1alpha2.Mapping {
	result := make([]requestv1alpha2.Mapping, len(mappings))
	for i, m := range mappings {
		result[i] = requestv1alpha2.Mapping{
			Method:  m.Method,
			Action:  m.Action,
			Body:    m.Body,
			URL:     m.URL,
			Headers: m.Headers,
		}
	}
	return result
}

func convertPayload(payload v1alpha2.Payload) requestv1alpha2.Payload {
	return requestv1alpha2.Payload{
		BaseUrl: payload.BaseUrl,
		Body:    payload.Body,
	}
}

func convertExpectedResponseCheck(check v1alpha2.ExpectedResponseCheck) requestv1alpha2.ExpectedResponseCheck {
	return requestv1alpha2.ExpectedResponseCheck{
		Type:  check.Type,
		Logic: check.Logic,
	}
}

func convertResponse(response v1alpha2.Response) requestv1alpha2.Response {
	return requestv1alpha2.Response{
		StatusCode: response.StatusCode,
		Body:       response.Body,
		Headers:    response.Headers,
	}
}

func convertCache(cache v1alpha2.Cache) requestv1alpha2.Cache {
	return requestv1alpha2.Cache{
		LastUpdated: cache.LastUpdated,
		Response: requestv1alpha2.Response{
			StatusCode: cache.Response.StatusCode,
			Body:       cache.Response.Body,
			Headers:    cache.Response.Headers,
		},
	}
}

func convertMapping(mapping v1alpha2.Mapping) requestv1alpha2.Mapping {
	return requestv1alpha2.Mapping{
		Method:  mapping.Method,
		Action:  mapping.Action,
		Body:    mapping.Body,
		URL:     mapping.URL,
		Headers: mapping.Headers,
	}
}

func (c *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr, ok := mg.(*v1alpha2.NamespacedRequest)
	if !ok {
		return managed.ExternalObservation{}, errors.New(errNotNamespacedRequest)
	}

	// Bridge to Request type for submodule compatibility
	bridgedRequest := c.bridgeToRequest(cr)

	observeRequestDetails, err := c.isUpToDate(ctx, bridgedRequest)
	if err != nil && err.Error() == observe.ErrObjectNotFound {
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, errFailedToCheckIfUpToDate)
	}

	// Get the latest version of the resource before updating
	if err := c.localKube.Get(ctx, types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}, cr); err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, errGetLatestVersion)
	}

	// Update bridged request with latest data
	bridgedRequest = c.bridgeToRequest(cr)

	statusHandler, err := statushandler.NewStatusHandler(ctx, bridgedRequest, observeRequestDetails.Details, observeRequestDetails.ResponseError, c.localKube, c.logger)
	if err != nil {
		return managed.ExternalObservation{}, err
	}

	synced := observeRequestDetails.Synced
	if synced {
		statusHandler.ResetFailures()
	}

	// Set status on bridged request
	bridgedRequest.Status.SetConditions(xpv1.Available())
	err = statusHandler.SetRequestStatus()
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, " failed updating status")
	}

	// Bridge back to NamespacedRequest
	c.bridgeFromRequest(cr, bridgedRequest)

	// Update the NamespacedRequest status
	cr.Status.SetConditions(xpv1.Available())
	if err := c.localKube.Status().Update(ctx, cr); err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, errFailedUpdateStatusConditions)
	}

	return managed.ExternalObservation{
		ResourceExists:    true,
		ResourceUpToDate:  synced,
		ConnectionDetails: nil,
	}, nil
}

// ObserveRequestDetails holds the result of an observation operation
type ObserveRequestDetails struct {
	Details       httpClient.HttpDetails
	ResponseError error
	Synced        bool
}

// NewObserve creates a new ObserveRequestDetails
func NewObserve(details httpClient.HttpDetails, resErr error, synced bool) ObserveRequestDetails {
	return ObserveRequestDetails{
		Synced:        synced,
		Details:       details,
		ResponseError: resErr,
	}
}

// FailedObserve creates a failed observation result
func FailedObserve() ObserveRequestDetails {
	return ObserveRequestDetails{
		Synced: false,
	}
}

// isUpToDate checks whether desired spec up to date with the observed state for a given request
func (c *external) isUpToDate(ctx context.Context, cr *requestv1alpha2.Request) (ObserveRequestDetails, error) {
	mapping, err := requestmapping.GetMapping(&cr.Spec.ForProvider, requestv1alpha2.ActionObserve, c.logger)
	if err != nil {
		return FailedObserve(), err
	}

	objectNotCreated := !c.isObjectValidForObservation(cr)

	// Evaluate the HTTP request template. If successfully templated, attempt to
	// observe the resource.
	requestDetails, err := requestgen.GenerateValidRequestDetails(ctx, cr, mapping, c.localKube, c.logger)
	if err != nil {
		if objectNotCreated {
			// The initial request was not successfully templated. Cannot
			// confirm existence of the resource, jumping to the default
			// behavior of creating before observing.
			err = errors.New(observe.ErrObjectNotFound)
		}
		return FailedObserve(), err
	}

	details, responseErr := c.http.SendRequest(ctx, mapping.Method, requestDetails.Url, requestDetails.Body, requestDetails.Headers, cr.Spec.ForProvider.InsecureSkipTLSVerify)
	// The initial observation of an object requires a successful HTTP response
	// to be considered existing.
	if !utils.IsHTTPSuccess(details.HttpResponse.StatusCode) && objectNotCreated {
		// Cannot confirm existence of the resource, jumping to the default
		// behavior of creating before observing.
		return FailedObserve(), errors.New(observe.ErrObjectNotFound)
	}
	if err := c.determineIfRemoved(ctx, cr, details, responseErr); err != nil {
		return FailedObserve(), err
	}

	datapatcher.ApplyResponseDataToSecrets(ctx, c.localKube, c.logger, &details.HttpResponse, cr.Spec.ForProvider.SecretInjectionConfigs, cr)
	return c.determineIfUpToDate(ctx, cr, details, responseErr)
}

// determineIfUpToDate determines if the object is up to date based on the response check.
func (c *external) determineIfUpToDate(ctx context.Context, cr *requestv1alpha2.Request, details httpClient.HttpDetails, responseErr error) (ObserveRequestDetails, error) {
	responseChecker := observe.GetIsUpToDateResponseCheck(cr, c.localKube, c.logger, c.http)
	if responseChecker == nil {
		return FailedObserve(), errors.Errorf(errExpectedResponseCheckType, "expectedResponseCheck")
	}

	result, err := responseChecker.Check(ctx, cr, details, responseErr)
	if err != nil {
		return FailedObserve(), err
	}

	return NewObserve(details, responseErr, result), nil
}

// determineIfRemoved determines if the object is removed based on the response check.
func (c *external) determineIfRemoved(ctx context.Context, cr *requestv1alpha2.Request, details httpClient.HttpDetails, responseErr error) error {
	responseChecker := observe.GetIsRemovedResponseCheck(cr, c.localKube, c.logger, c.http)
	if responseChecker == nil {
		return errors.Errorf(errExpectedResponseCheckType, "isRemovedCheck")
	}

	return responseChecker.Check(ctx, cr, details, responseErr)
}

// isObjectValidForObservation checks if the object is valid for observation
func (c *external) isObjectValidForObservation(cr *requestv1alpha2.Request) bool {
	return cr.Status.Response.StatusCode != 0 &&
		!(cr.Status.RequestDetails.Method == "POST" && utils.IsHTTPError(cr.Status.Response.StatusCode))
}

// deployAction executes the action based on the given NamespacedRequest resource and Mapping configuration.
func (c *external) deployAction(ctx context.Context, cr *v1alpha2.NamespacedRequest, action string) error {
	// Bridge to Request type for submodule compatibility
	bridgedRequest := c.bridgeToRequest(cr)

	mapping, err := requestmapping.GetMapping(&bridgedRequest.Spec.ForProvider, action, c.logger)
	if err != nil {
		c.logger.Info(err.Error())
		return nil
	}

	requestDetails, err := requestgen.GenerateValidRequestDetails(ctx, bridgedRequest, mapping, c.localKube, c.logger)
	if err != nil {
		return err
	}

	details, err := c.http.SendRequest(ctx, mapping.Method, requestDetails.Url, requestDetails.Body, requestDetails.Headers, bridgedRequest.Spec.ForProvider.InsecureSkipTLSVerify)
	datapatcher.ApplyResponseDataToSecrets(ctx, c.localKube, c.logger, &details.HttpResponse, bridgedRequest.Spec.ForProvider.SecretInjectionConfigs, bridgedRequest)

	statusHandler, err := statushandler.NewStatusHandler(ctx, bridgedRequest, details, err, c.localKube, c.logger)
	if err != nil {
		return err
	}

	err = statusHandler.SetRequestStatus()
	if err != nil {
		return err
	}

	// Bridge back to NamespacedRequest
	c.bridgeFromRequest(cr, bridgedRequest)

	// Update the NamespacedRequest status
	return c.localKube.Status().Update(ctx, cr)
}

func (c *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr, ok := mg.(*v1alpha2.NamespacedRequest)
	if !ok {
		return managed.ExternalCreation{}, errors.New(errNotNamespacedRequest)
	}

	return managed.ExternalCreation{}, errors.Wrap(c.deployAction(ctx, cr, v1alpha2.ActionCreate), errFailedToSendHttpRequest)
}

func (c *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr, ok := mg.(*v1alpha2.NamespacedRequest)
	if !ok {
		return managed.ExternalUpdate{}, errors.New(errNotNamespacedRequest)
	}

	return managed.ExternalUpdate{}, errors.Wrap(c.deployAction(ctx, cr, v1alpha2.ActionUpdate), errFailedToSendHttpRequest)
}

func (c *external) Delete(ctx context.Context, mg resource.Managed) (managed.ExternalDelete, error) {
	cr, ok := mg.(*v1alpha2.NamespacedRequest)
	if !ok {
		return managed.ExternalDelete{}, errors.New(errNotNamespacedRequest)
	}

	return managed.ExternalDelete{}, errors.Wrap(c.deployAction(ctx, cr, v1alpha2.ActionRemove), errFailedToSendHttpRequest)
}

// Disconnect does nothing. It never returns an error.
func (c *external) Disconnect(_ context.Context) error {
	return nil
}
