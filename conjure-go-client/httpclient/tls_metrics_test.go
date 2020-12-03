// Copyright (c) 2020 Palantir Technologies. All rights reserved.
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

package httpclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient"
	"github.com/palantir/pkg/metrics"
	"github.com/palantir/pkg/tlsconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTLSMetricsMiddleware_SuccessfulHandshake(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()

	rootRegistry := metrics.NewRootMetricsRegistry()
	ctx := metrics.WithRegistry(context.Background(), rootRegistry)
	tlsConf, err := tlsconfig.NewClientConfig(tlsconfig.ClientRootCAs(tlsconfig.CertPoolFromCerts(srv.Certificate())))
	require.NoError(t, err)
	client, err := httpclient.NewHTTPClient(httpclient.WithServiceName("test-service"), httpclient.WithTLSConfig(tlsConf))
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	req = req.WithContext(httpclient.ContextWithRPCMethodName(ctx, "test-endpoint"))

	_, err = client.Do(req)
	require.NoError(t, err)

	attempt := false
	success := false
	failure := false
	rootRegistry.Each(func(name string, tags metrics.Tags, value metrics.MetricVal) {
		switch name {
		case httpclient.MetricTLSHandshakeAttempt:
			attempt = true
		case httpclient.MetricTLSHandshakeFailure:
			failure = true
		case httpclient.MetricTLSHandshake:
			success = true
			tagMap := tags.ToMap()
			_, ok := tagMap[httpclient.CipherTagKey]
			assert.True(t, ok)
			_, ok = tagMap[httpclient.TLSVersionTagKey]
			assert.True(t, ok)
			_, ok = tagMap[httpclient.NextProtocolTagKey]
			assert.True(t, ok)
		}
	})
	assert.True(t, attempt, "no tls handshake attempt registered")
	assert.True(t, success, "no successful tls handshake attempt registered")
	assert.False(t, failure, "failed tls handshake attempt registered")
}

func TestTLSMetricsMiddleware_FailedHandshake(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()

	rootRegistry := metrics.NewRootMetricsRegistry()
	ctx := metrics.WithRegistry(context.Background(), rootRegistry)
	client, err := httpclient.NewHTTPClient(httpclient.WithServiceName("test-service"))
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	req = req.WithContext(httpclient.ContextWithRPCMethodName(ctx, "test-endpoint"))

	_, err = client.Do(req)
	require.Error(t, err)

	attempt := false
	success := false
	failure := false
	rootRegistry.Each(func(name string, tags metrics.Tags, value metrics.MetricVal) {
		switch name {
		case httpclient.MetricTLSHandshakeAttempt:
			attempt = true
		case httpclient.MetricTLSHandshakeFailure:
			failure = true
		case httpclient.MetricTLSHandshake:
			success = true
		}
	})
	assert.True(t, attempt, "no tls handshake attempt registered")
	assert.False(t, success, "successful tls handshake attempt registered")
	assert.True(t, failure, "no failed tls handshake attempt registered")
}
