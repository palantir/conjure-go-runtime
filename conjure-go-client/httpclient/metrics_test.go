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

package httpclient_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient"
	"github.com/palantir/pkg/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTagsProviderFunc struct {
	key, val string
}

func (ftpf fakeTagsProviderFunc) Tags(_ *http.Request, _ *http.Response) metrics.Tags {
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
		tagsProviders []httpclient.TagsProvider
		expectedTags  metrics.Tags
	}{
		{
			name: "with custom tag",
			tagsProviders: []httpclient.TagsProvider{
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
			tagsProviders: []httpclient.TagsProvider{
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

	rpcNamesAndExpectedTags := map[string]string{
		"MethodName": "MethodName",
		"":           "RPCMethodNameMissing",
		// This is silly, but we want to trigger an error in tag creation. However right now the only validation that is not broken in the length check
		"fdsafdasfdsafdsafdsafdfasfdsafdasfdsafdsafdsafdfasfdsafdasfdsafdsafdsafdfasfdsafdasfdsafdsafdsafdfasfdsafdasfdsafdsafdsafdfasfdsafdasfdsafdsafdsafdfasfdsafdasfdsafdsafdsafdfasfdsafdasfdsafdsafdsafdfasfdsafdasfdsafdsafdsafdfasfdsafdasfdsafdsafdsafdfas": "RPCMethodNameInvalid",
	}

	test := func(t *testing.T, method string, tagsProviders []httpclient.TagsProvider, statusCode int, statusFamily string, customTags metrics.Tags, getRpcNameAndExpectedTag func() (string, string)) {
		rootRegistry := metrics.NewRootMetricsRegistry()
		ctx := metrics.WithRegistry(context.Background(), rootRegistry)

		// create server
		var serverURLstr string
		if statusCode > 0 {
			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(statusCode)
			}))
			defer server.Close()
			serverURLstr = server.URL
		} else {
			serverURLstr = "http://not-a-real-host:12345"
		}

		// create client
		client, err := httpclient.NewClient(
			httpclient.WithHTTPTimeout(time.Second),
			httpclient.WithServiceName("my-service"),
			httpclient.WithMetrics(tagsProviders...),
			httpclient.WithBaseURLs([]string{serverURLstr}))
		require.NoError(t, err)

		rpcMethodName, expectedMethodNameTag := getRpcNameAndExpectedTag()
		// do request
		_, err = client.Do(ctx, httpclient.WithRequestMethod(method), httpclient.WithRPCMethodName(rpcMethodName))
		if statusCode < 200 || statusCode > 299 {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}

		// assert metrics exist in registry
		var metricName string
		var metricTags metrics.Tags
		rootRegistry.Each(metrics.MetricVisitor(func(name string, tags metrics.Tags, _ metrics.MetricVal) {
			metricName = name
			metricTags = tags
		}))
		assert.Equal(t, "client.response", metricName)
		expectedTags := append(
			customTags,
			metrics.MustNewTag("method", method),
			metrics.MustNewTag("family", statusFamily),
			metrics.MustNewTag("service-name", "my-service"),
			metrics.MustNewTag("method-name", expectedMethodNameTag))
		assert.Equal(t, expectedTags.ToMap(), metricTags.ToMap())
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

	client, err := httpclient.NewHTTPClient(httpclient.WithServiceName("test-service"), httpclient.WithMetrics())
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	req = req.WithContext(httpclient.ContextWithRPCMethodName(ctx, "test-endpoint"))

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
