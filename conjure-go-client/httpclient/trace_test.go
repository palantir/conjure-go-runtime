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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/palantir/conjure-go-runtime/conjure-go-client/httpclient"
	"github.com/palantir/witchcraft-go-tracing/wtracing"
	"github.com/palantir/witchcraft-go-tracing/wtracing/propagation/b3"
	"github.com/palantir/witchcraft-go-tracing/wzipkin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTracing(t *testing.T) {
	for _, testCase := range []struct {
		name                 string
		requestParams        []httpclient.RequestParam
		ongoingSpanName      string
		shouldPropagateTrace bool
	}{
		{
			name: "no ongoing span, no RPC name",
			requestParams: []httpclient.RequestParam{
				httpclient.WithRequestMethod(http.MethodGet),
			},
			shouldPropagateTrace: false,
		},
		{
			name: "ongoing span",
			requestParams: []httpclient.RequestParam{
				httpclient.WithRequestMethod(http.MethodGet),
			},
			ongoingSpanName:      "operation",
			shouldPropagateTrace: true,
		},
		{
			name: "ongoing span, with RPC name",
			requestParams: []httpclient.RequestParam{
				httpclient.WithRequestMethod(http.MethodGet),
				httpclient.WithRPCMethodName("myname"),
			},
			ongoingSpanName:      "operation",
			shouldPropagateTrace: true,
		},
		{
			name: "no ongoing span, RPC name",
			requestParams: []httpclient.RequestParam{
				httpclient.WithRequestMethod(http.MethodGet),
				httpclient.WithRPCMethodName("myname"),
			},
			shouldPropagateTrace: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tracer := mustNewTracer()
			ctx := wtracing.ContextWithTracer(context.Background(), tracer)

			if testCase.ongoingSpanName != "" {
				ctx = wtracing.ContextWithSpan(ctx, tracer.StartSpan(testCase.ongoingSpanName))
			}

			// setup server to either extract or not extract a trace
			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				spanCtx := b3.SpanExtractor(req)()
				if testCase.shouldPropagateTrace {
					assert.NoError(t, spanCtx.Err)
				} else {
					assert.Error(t, spanCtx.Err)
				}
				rw.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client, err := httpclient.NewClient(httpclient.WithBaseURLs([]string{server.URL}))
			require.NoError(t, err)

			resp, err := client.Do(ctx, testCase.requestParams...)
			require.NoError(t, err)
			assert.NotNil(t, resp)
		})
	}
}

func mustNewTracer() wtracing.Tracer {
	tracer, err := wzipkin.NewTracer(&testReporter{reporterMap: map[string]interface{}{}})
	if err != nil {
		panic(err)
	}
	return tracer
}

type testReporter struct {
	reporterMap map[string]interface{}
}

func (r *testReporter) Send(span wtracing.SpanModel) {
	r.reporterMap["traceID"] = span.TraceID
	r.reporterMap["spanID"] = span.ID
	r.reporterMap["parentID"] = span.ParentID
	r.reporterMap["debug"] = span.Debug
	r.reporterMap["sampled"] = span.Sampled
	r.reporterMap["err"] = span.Err

	r.reporterMap["name"] = span.Name
	r.reporterMap["kind"] = span.Kind
	r.reporterMap["timestamp"] = span.Timestamp
	r.reporterMap["duration"] = span.Duration
	r.reporterMap["localEndpoint"] = span.LocalEndpoint
	r.reporterMap["remoteEndpoint"] = span.RemoteEndpoint
}

func (r *testReporter) Close() error {
	return nil
}
