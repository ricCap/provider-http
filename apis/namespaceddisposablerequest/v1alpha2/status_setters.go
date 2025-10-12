package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (d *NamespacedDisposableRequest) SetStatusCode(statusCode int) {
	d.Status.Response.StatusCode = statusCode
}

func (d *NamespacedDisposableRequest) SetHeaders(headers map[string][]string) {
	d.Status.Response.Headers = headers
}

func (d *NamespacedDisposableRequest) SetBody(body string) {
	d.Status.Response.Body = body
}

func (d *NamespacedDisposableRequest) SetError(err error) {
	d.Status.Failed++
	if err != nil {
		d.Status.Error = err.Error()
	}
}

func (d *NamespacedDisposableRequest) ResetFailures() {
	d.Status.Failed = 0
	d.Status.Error = ""
}

func (d *NamespacedDisposableRequest) SetSynced(status bool) {
	d.Status.Synced = status
}

func (d *NamespacedDisposableRequest) SetRequestDetails(url, method, body string, headers map[string][]string) {
	d.Status.RequestDetails.Body = body
	d.Status.RequestDetails.URL = url
	d.Status.RequestDetails.Headers = headers
	d.Status.RequestDetails.Method = method
}

func (d *NamespacedDisposableRequest) SetLastReconcileTime(t metav1.Time) {
	d.Status.LastReconcileTime = t
}