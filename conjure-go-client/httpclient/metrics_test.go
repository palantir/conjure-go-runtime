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
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/palantir/pkg/metrics"
	"github.com/palantir/pkg/tlsconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTagsProviderFunc struct {
	key, val string
}

func (ftpf fakeTagsProviderFunc) Tags(_ *http.Request, _ *http.Response, _ error) metrics.Tags {
	return metrics.Tags{
		metrics.MustNewTag(ftpf.key, ftpf.val),
	}
}

func TestRoundTripperWithMetrics(t *testing.T) {
	testMethods := []string{
		"GET",
		"PUT",
		"POST",
		"PATCH",
		"DELETE",
	}

	testProviders := []struct {
		name          string
		tagsProviders []TagsProvider
		expectedTags  metrics.Tags
	}{
		{
			name: "with custom tag",
			tagsProviders: []TagsProvider{
				fakeTagsProviderFunc{
					key: "foo",
					val: "bar",
				},
			},
			expectedTags: metrics.Tags{
				metrics.MustNewTag("foo", "bar"),
			},
		},
		{
			name: "with many custom tags",
			tagsProviders: []TagsProvider{
				fakeTagsProviderFunc{
					key: "foo",
					val: "bar",
				},
				fakeTagsProviderFunc{
					key: "bar",
					val: "baz",
				},
			},
			expectedTags: metrics.Tags{
				metrics.MustNewTag("foo", "bar"),
				metrics.MustNewTag("bar", "baz"),
			},
		},
		{
			name:          "without custom tags",
			tagsProviders: nil,
			expectedTags:  nil,
		},
	}

	testStatusCode := map[int]string{
		200: "2xx",
		500: "5xx",
		0:   "other",
	}
	expectedMetricValue := map[int]int64{
		200: 1_000,
		500: 1_000,
		0:   0,
	}

	rpcNamesAndExpectedTags := map[string]string{
		"MethodName": "MethodName",
		"":           "RPCMethodNameMissing",
		// This is silly, but we want to trigger an error in tag creation. However right now the only validation that is not broken in the length check
		"fdsafdasfdsafdsafdsafdfasfdsafdasfdsafdsafdsafdfasfdsafdasfdsafdsafdsafdfasfdsafdasfdsafdsafdsafdfasfdsafdasfdsafdsafdsafdfasfdsafdasfdsafdsafdsafdfasfdsafdasfdsafdsafdsafdfasfdsafdasfdsafdsafdsafdfasfdsafdasfdsafdsafdsafdfasfdsafdasfdsafdsafdsafdfas": "RPCMethodNameInvalid",
	}

	test := func(t *testing.T, method string, tagsProviders []TagsProvider, statusCode int, statusFamily string, customTags metrics.Tags, expectedValue int64, getRpcNameAndExpectedTag func() (string, string)) {
		rootRegistry := metrics.NewRootMetricsRegistry()
		ctx := metrics.WithRegistry(context.Background(), rootRegistry)

		// create server
		var serverURLstr string
		now = func() time.Time { return time.UnixMilli(0) }
		if statusCode > 0 {
			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				now = func() time.Time { return time.UnixMilli(1) }
				rw.WriteHeader(statusCode)
			}))
			defer server.Close()
			serverURLstr = server.URL
		} else {
			serverURLstr = "http://not-a-real-host:12345"
		}

		// create client
		client, err := NewClient(
			WithHTTPTimeout(5*time.Second),
			WithServiceName("my-service"),
			WithMetrics(tagsProviders...),
			WithBaseURLs([]string{serverURLstr}))
		require.NoError(t, err)

		rpcMethodName, expectedMethodNameTag := getRpcNameAndExpectedTag()
		// do request
		_, err = client.Do(ctx, WithRequestMethod(method), WithRPCMethodName(rpcMethodName))
		if statusCode < 200 || statusCode > 299 {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}

		// assert metrics exist in registry
		var metricName string
		var metricTags metrics.Tags
		var metricValue int64
		rootRegistry.Each(func(name string, tags metrics.Tags, value metrics.MetricVal) {
			metricName = name
			metricTags = tags
			v, ok := value.Values()["max"]
			if ok {
				metricValue = v.(int64)
			}
		})
		assert.Equal(t, "client.response", metricName)
		expectedTags := append(
			customTags,
			metrics.MustNewTag("method", method),
			metrics.MustNewTag("family", statusFamily),
			metrics.MustNewTag("service-name", "my-service"),
			metrics.MustNewTag("method-name", expectedMethodNameTag))
		assert.Equal(t, expectedTags.ToMap(), metricTags.ToMap())
		assert.Equal(t, expectedValue, metricValue)
	}
	for _, testMethod := range testMethods {
		t.Run(testMethod, func(t *testing.T) {
			for _, testProvider := range testProviders {
				t.Run(testProvider.name, func(t *testing.T) {
					for testStatusCode, testStatusFamily := range testStatusCode {
						t.Run(fmt.Sprintf("%d", testStatusCode), func(t *testing.T) {
							for rpcName, expectedTagValue := range rpcNamesAndExpectedTags {
								t.Run(rpcName, func(t *testing.T) {
									test(t,
										testMethod,
										testProvider.tagsProviders,
										testStatusCode,
										testStatusFamily,
										testProvider.expectedTags,
										expectedMetricValue[testStatusCode],
										func() (string, string) { return rpcName, expectedTagValue },
									)
								})
							}
						})
					}
				})
			}
		})
	}
}

func TestMetricsMiddleware_HTTPClient(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	rootRegistry := metrics.NewRootMetricsRegistry()
	ctx := metrics.WithRegistry(context.Background(), rootRegistry)

	client, err := NewHTTPClient(WithServiceName("test-service"), WithMetrics())
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	req = req.WithContext(ContextWithRPCMethodName(ctx, "test-endpoint"))

	_, err = client.Do(req)
	require.NoError(t, err)

	found := false
	rootRegistry.Each(func(name string, tags metrics.Tags, value metrics.MetricVal) {
		if name != "client.response" {
			return
		}
		found = true
		expectedTags := map[metrics.Tag]struct{}{
			metrics.MustNewTag("family", "4xx"):                {},
			metrics.MustNewTag("method", "get"):                {},
			metrics.MustNewTag("method-name", "test-endpoint"): {},
			metrics.MustNewTag("service-name", "test-service"): {},
		}
		assert.Equal(t, expectedTags, tags.ToSet())
	})
	assert.True(t, found, "did not find client.response metric")
}

func TestMetricsMiddleware_ClientTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(time.Second)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	rootRegistry := metrics.NewRootMetricsRegistry()
	ctx := metrics.WithRegistry(context.Background(), rootRegistry)

	client, err := NewClient(
		WithBaseURLs([]string{srv.URL}),
		WithTLSInsecureSkipVerify(),
		WithServiceName("test-service"),
		WithHTTPTimeout(time.Millisecond),
		WithMetrics())
	require.NoError(t, err)

	_, err = client.Get(ctx, WithRPCMethodName("test-endpoint"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "Client.Timeout exceeded while awaiting headers")

	found := false
	rootRegistry.Each(func(name string, tags metrics.Tags, value metrics.MetricVal) {
		if name != "client.response" {
			return
		}
		found = true
		expectedTags := map[metrics.Tag]struct{}{
			metrics.MustNewTag("family", "timeout"):            {},
			metrics.MustNewTag("method", "get"):                {},
			metrics.MustNewTag("method-name", "test-endpoint"): {},
			metrics.MustNewTag("service-name", "test-service"): {},
		}
		assert.Equal(t, expectedTags, tags.ToSet(), "expected timeout tags for %v", err)
	})
	assert.True(t, found, "did not find client.response metric")
}

func TestMetricsMiddleware_ContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	rootRegistry := metrics.NewRootMetricsRegistry()
	ctx := metrics.WithRegistry(context.Background(), rootRegistry)
	ctx, cancel := context.WithCancel(ctx)
	cancel()

	client, err := NewClient(
		WithBaseURLs([]string{srv.URL}),
		WithTLSInsecureSkipVerify(),
		WithServiceName("test-service"),
		WithMetrics())
	require.NoError(t, err)

	_, err = client.Get(ctx, WithRPCMethodName("test-endpoint"))
	require.EqualError(t, err, "httpclient request failed: context canceled")

	found := false
	rootRegistry.Each(func(name string, tags metrics.Tags, value metrics.MetricVal) {
		if name != "client.response" {
			return
		}
		found = true
		expectedTags := map[metrics.Tag]struct{}{
			metrics.MustNewTag("family", "timeout"):            {},
			metrics.MustNewTag("method", "get"):                {},
			metrics.MustNewTag("method-name", "test-endpoint"): {},
			metrics.MustNewTag("service-name", "test-service"): {},
		}
		assert.Equal(t, expectedTags, tags.ToSet(), "expected timeout tags for %v", err)
	})
	assert.True(t, found, "did not find client.response metric")
}

func TestMetricsMiddleware_SuccessfulTLSHandshake(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()

	rootRegistry := metrics.NewRootMetricsRegistry()
	ctx := metrics.WithRegistry(context.Background(), rootRegistry)
	tlsConf, err := tlsconfig.NewClientConfig(tlsconfig.ClientRootCAs(tlsconfig.CertPoolFromCerts(srv.Certificate())))
	require.NoError(t, err)
	client, err := NewHTTPClient(WithServiceName("test-service"), WithMetrics(), WithTLSConfig(tlsConf))
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	req = req.WithContext(ContextWithRPCMethodName(ctx, "test-endpoint"))

	_, err = client.Do(req)
	require.NoError(t, err)

	attempt := false
	success := false
	failure := false
	rootRegistry.Each(func(name string, tags metrics.Tags, value metrics.MetricVal) {
		tagMap := tags.ToMap()
		switch name {
		case MetricTLSHandshakeAttempt:
			attempt = true
			svcName, ok := tagMap[MetricTagServiceName]
			assert.True(t, ok)
			assert.Equal(t, svcName, "test-service")
		case MetricTLSHandshakeFailure:
			failure = true
		case MetricTLSHandshake:
			success = true
			svcName, ok := tagMap[MetricTagServiceName]
			assert.True(t, ok)
			assert.Equal(t, svcName, "test-service")
			_, ok = tagMap[CipherTagKey]
			assert.True(t, ok)
			_, ok = tagMap[TLSVersionTagKey]
			assert.True(t, ok)
			_, ok = tagMap[NextProtocolTagKey]
			assert.True(t, ok)
		}
	})
	assert.True(t, attempt, "no tls handshake attempt registered")
	assert.True(t, success, "no successful tls handshake attempt registered")
	assert.False(t, failure, "failed tls handshake attempt registered")
}

func TestMetricsMiddleware_FailedTLSHandshake(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()

	rootRegistry := metrics.NewRootMetricsRegistry()
	ctx := metrics.WithRegistry(context.Background(), rootRegistry)
	client, err := NewHTTPClient(WithServiceName("test-service"), WithMetrics())
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	req = req.WithContext(ContextWithRPCMethodName(ctx, "test-endpoint"))

	_, err = client.Do(req)
	require.Error(t, err)

	attempt := false
	success := false
	failure := false
	rootRegistry.Each(func(name string, tags metrics.Tags, value metrics.MetricVal) {
		tagMap := tags.ToMap()
		switch name {
		case MetricTLSHandshakeAttempt:
			attempt = true
			svcName, ok := tagMap[MetricTagServiceName]
			assert.True(t, ok)
			assert.Equal(t, svcName, "test-service")
		case MetricTLSHandshakeFailure:
			failure = true
			svcName, ok := tagMap[MetricTagServiceName]
			assert.True(t, ok)
			assert.Equal(t, svcName, "test-service")
		case MetricTLSHandshake:
			success = true
		}
	})
	assert.True(t, attempt, "no tls handshake attempt registered")
	assert.False(t, success, "successful tls handshake attempt registered")
	assert.True(t, failure, "no failed tls handshake attempt registered")
}

func TestMetricsMiddleware_InFlightRequests(t *testing.T) {
	rootRegistry := metrics.NewRootMetricsRegistry()
	ctx := metrics.WithRegistry(context.Background(), rootRegistry)
	serviceNameTag := metrics.MustNewTag("service-name", "test-service")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientMetric := rootRegistry.Counter(MetricRequestInFlight, serviceNameTag).Count()
		assert.Equal(t, int64(1), clientMetric, "%s should be nonzero during a request", MetricRequestInFlight)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	client, err := NewClient(
		WithBaseURLs([]string{srv.URL}),
		WithServiceName("test-service"),
		WithMetrics())
	require.NoError(t, err)

	_, err = client.Get(ctx)
	require.NoError(t, err)
	newConnsMetric := rootRegistry.Counter(MetricConnCreate, MetricTagConnectionNew, serviceNameTag)
	reusedConnsMetric := rootRegistry.Counter(MetricConnCreate, MetricTagConnectionReused, serviceNameTag)
	assert.Equal(t, int64(1), newConnsMetric.Count(), "%s|reused:false should be 1 after a request", MetricConnCreate)
	assert.Equal(t, int64(0), reusedConnsMetric.Count(), "%s|reused:true should be 0 after the first request", MetricConnCreate)

	// do the request a second time to assert the connection is reused
	_, err = client.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), newConnsMetric.Count(), "%s|reused:false should be 1 after a second request due to reuse", MetricConnCreate)
	assert.Equal(t, int64(1), reusedConnsMetric.Count(), "%s|reused:true should be 1 after a request", MetricConnCreate)

	clientMetric := rootRegistry.Counter(MetricRequestInFlight, serviceNameTag)
	assert.Equal(t, int64(0), clientMetric.Count(), "%s should be zero after a request", MetricRequestInFlight)
}
