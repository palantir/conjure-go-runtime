// Copyright (c) 2018 Palantir Technologies. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package httpclient

import (
	"net/http"
	"time"

	"github.com/palantir/pkg/metrics"
	"github.com/palantir/witchcraft-go-error"
)

const (
	metricClientResponse = "client.response"
	metricTagFamily      = "family"
	metricTagMethod      = "method"
	metricRPCMethodName  = "method-name"
	metricTagServiceName = "service-name"

	metricTagFamilyOther = "other"
	metricTagFamily1xx   = "1xx"
	metricTagFamily2xx   = "2xx"
	metricTagFamily3xx   = "3xx"
	metricTagFamily4xx   = "4xx"
	metricTagFamily5xx   = "5xx"
)

// A TagsProvider returns metrics tags based on an http round trip.
type TagsProvider interface {
	Tags(*http.Request, *http.Response) metrics.Tags
}

// TagsProviderFunc is a convenience type that implements TagsProvider.
type TagsProviderFunc func(*http.Request, *http.Response) metrics.Tags

func (f TagsProviderFunc) Tags(req *http.Request, resp *http.Response) metrics.Tags {
	return f(req, resp)
}

// MetricsMiddleware updates the "client.response" timer metric on every request.
// By default, metrics are tagged with 'service-name', 'method', and 'family' (of the
// status code). This metric name and tag set matches http-remoting's DefaultHostMetrics:
// https://github.com/palantir/http-remoting/blob/develop/okhttp-clients/src/main/java/com/palantir/remoting3/okhttp/DefaultHostMetrics.java
func MetricsMiddleware(serviceName string, tagProviders ...TagsProvider) (Middleware, error) {
	serviceNameTag, err := metrics.NewTag(metricTagServiceName, serviceName)
	if err != nil {
		return nil, werror.Wrap(err, "failed to construct service-name metric tag", werror.SafeParam("serviceName", serviceName))
	}
	return &metricsMiddleware{Tags: append(
		tagProviders,
		TagsProviderFunc(tagStatusFamily),
		TagsProviderFunc(tagRequestMethod),
		TagsProviderFunc(tagRequestMethodName),
		TagsProviderFunc(func(*http.Request, *http.Response) metrics.Tags { return metrics.Tags{serviceNameTag} }),
	)}, nil
}

type metricsMiddleware struct {
	Tags []TagsProvider
}

// RoundTrip will emit counter and timer metrics with the name 'mariner.k8sClient.request'
// and k8s for API group, API version, namespace, resource kind, request method, and response status code.
func (h *metricsMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	start := time.Now()
	resp, err := next.RoundTrip(req)
	duration := time.Since(start)

	var tags metrics.Tags
	for _, tagProvider := range h.Tags {
		tags = append(tags, tagProvider.Tags(req, resp)...)
	}

	metrics.FromContext(req.Context()).Timer(metricClientResponse, tags...).Update(duration)
	return resp, err
}

func tagStatusFamily(_ *http.Request, resp *http.Response) metrics.Tags {
	var tag metrics.Tag
	switch {
	case resp == nil, resp.StatusCode < 100, resp.StatusCode > 599:
		tag = metrics.MustNewTag(metricTagFamily, metricTagFamilyOther)
	case resp.StatusCode < 200:
		tag = metrics.MustNewTag(metricTagFamily, metricTagFamily1xx)
	case resp.StatusCode < 300:
		tag = metrics.MustNewTag(metricTagFamily, metricTagFamily2xx)
	case resp.StatusCode < 400:
		tag = metrics.MustNewTag(metricTagFamily, metricTagFamily3xx)
	case resp.StatusCode < 500:
		tag = metrics.MustNewTag(metricTagFamily, metricTagFamily4xx)
	case resp.StatusCode < 600:
		tag = metrics.MustNewTag(metricTagFamily, metricTagFamily5xx)
	}
	return metrics.Tags{tag}
}

func tagRequestMethod(req *http.Request, _ *http.Response) metrics.Tags {
	return metrics.Tags{metrics.MustNewTag(metricTagMethod, req.Method)}
}

func tagRequestMethodName(req *http.Request, _ *http.Response) metrics.Tags {
	rpcMethodName := getRPCMethodName(req.Context())
	if rpcMethodName == "" {
		return metrics.Tags{metrics.MustNewTag(metricRPCMethodName, "RPCMethodNameMissing")}
	}
	tag, err := metrics.NewTag(metricRPCMethodName, rpcMethodName)
	if err == nil {
		return metrics.Tags{tag}
	}
	return metrics.Tags{metrics.MustNewTag(metricRPCMethodName, "RPCMethodNameInvalid")}
}
