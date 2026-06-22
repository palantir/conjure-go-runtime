// Copyright (c) 2026 Palantir Technologies. All rights reserved.
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

package httpc_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
)

// testPayload is the shared JSON request/response body for tests that only need a
// trivial typed value to encode and decode.
type testPayload struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

// roundTripFunc adapts a function to an http.RoundTripper and, via Send, to a
// one-method httpc.Runtime. As a transport it returns whatever the func produces;
// as a Runtime its Send routes through a bare runtime that uses the func as its
// transport against a dummy base URL — for tests that return canned responses
// without binding a real server.
type roundTripFunc struct {
	fn func(*http.Request) (*http.Response, error)
}

func (f *roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f.fn(req) }

func (f *roundTripFunc) Send(ctx context.Context, req *http.Request, opts httpc.SendOptions) (*http.Response, error) {
	client, err := newBareClient(ctx, "http://localhost", f)
	if err != nil {
		return nil, err
	}
	return client.Send(ctx, req, opts)
}

// emptyResponse is the canned 204/no-body response an in-process [roundTripFunc]
// returns when the test only asserts on the outgoing request.
func emptyResponse(req *http.Request) *http.Response {
	return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody, Request: req}
}

// httpTestClient wraps an httptest.Server as a one-method httpc.Runtime: Send
// routes through a runtime pointed at the server, with telemetry and retry
// backoff disabled so requests reach the server unchanged and tests stay fast.
type httpTestClient struct {
	server *httptest.Server
}

func (c *httpTestClient) Send(ctx context.Context, req *http.Request, opts httpc.SendOptions) (*http.Response, error) {
	client, err := newBareClient(ctx, c.server.URL, c.server.Client().Transport)
	if err != nil {
		return nil, err
	}
	return client.Send(ctx, req, opts)
}

// newBareClient builds a runtime routing through transport against baseURL with
// telemetry and retry backoff disabled, mirroring the minimal no-middleware test
// client these tests relied on before Runtime.Send existed.
func newBareClient(ctx context.Context, baseURL string, transport http.RoundTripper) (httpc.Runtime, error) {
	return httpc.NewBuilder().
		SetBaseURLs(baseURL).
		SetTransport(transport).
		SetInitialBackoff(0).
		SetMaxBackoff(0).
		DisableTracing().
		DisableTraceHeaderPropagation().
		DisableClientTraceMetrics().
		DisablePanicRecovery().
		SetDisableMetrics(true).
		Build(ctx)
}

// newTestServer creates an httptest.Server and registers cleanup.
func newTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

// recordingMiddleware appends name+"-before" before delegating to next and
// name+"-after" afterward into order, so tests can assert middleware nesting.
func recordingMiddleware(order *[]string, name string) httpc.Middleware {
	return httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		*order = append(*order, name+"-before")
		resp, err := next.RoundTrip(req)
		*order = append(*order, name+"-after")
		return resp, err
	})
}
