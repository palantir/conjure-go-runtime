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

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/palantir/pkg/metrics"
)

const (
	MetricTagServiceName = "service-name"

	MetricTLSHandshakeAttempt = "tls.handshake.attempt"
	MetricTLSHandshakeFailure = "tls.handshake.failure"
	MetricTLSHandshake        = "tls.handshake"
	CipherTagKey              = "cipher"
	NextProtocolTagKey        = "next_protocol"
	TLSVersionTagKey          = "tls_version"

	MetricConnCreate      = "client.connection.create" // monotonic counter of each new request, tagged with reused:true or reused:false
	MetricRequestInFlight = "client.request.in-flight"
)

var (
	MetricTagConnectionNew    = metrics.MustNewTag("reused", "false")
	MetricTagConnectionReused = metrics.MustNewTag("reused", "true")
)

// A TagsProvider returns metrics tags based on an http round trip.
// The 'error' argument is that returned from the request (if any).
type TagsProvider = httpc.TagsProvider

// TagsProviderFunc is a convenience type that implements TagsProvider.
type TagsProviderFunc = httpc.TagsProviderFunc

type StaticTagsProvider metrics.Tags

func (s StaticTagsProvider) Tags(_ *http.Request, _ *http.Response, _ error) metrics.Tags {
	return metrics.Tags(s)
}

// MetricsMiddleware updates the "client.response" timer metric on every request.
// By default, metrics are tagged with 'service-name', 'method', and 'family' (of the
// status code). This metric name and tag set matches http-remoting's DefaultHostMetrics:
// https://github.com/palantir/http-remoting/blob/develop/okhttp-clients/src/main/java/com/palantir/remoting3/okhttp/DefaultHostMetrics.java
var MetricsMiddleware = httpc.MetricsMiddleware
