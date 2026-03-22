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

package httpc

import (
	"context"
	"net/http"
)

// Client is the minimal transport interface returned by ServiceBuilder.Build and ClientBuilder.Build.
// It wraps an http.RoundTripper with the configured middleware stack, retries, and URI scoring.
//
// Endpoint.Execute builds the *http.Request (method, path, headers, encoded body),
// sets the context via req.WithContext, and calls Client.Do. Response decoding and
// per-request error handling are performed by the Endpoint after the round-trip completes.
//
// The signature matches *http.Client.Do, so a plain *http.Client satisfies this
// interface and can be used directly wherever a Client is expected.
type Client interface {
	// Do executes an HTTP request.
	//
	// If the Client is configured with base URLs, it selects one (via URI scoring)
	// and prepends it to the request's path on each attempt. On retries, a different
	// base URI may be selected. If no base URLs are configured and the request
	// already has a fully qualified URL, the Client uses it as-is.
	//
	// The Client applies the configured middleware stack, enforces timeouts, and
	// handles retry logic (including re-reading the request body) transparently.
	// The request's context (req.Context()) is used for cancellation and deadlines.
	Do(req *http.Request) (*http.Response, error)
}

// ConfigurableClient is a Client that retains its builder configuration.
// Call Builder to obtain a new builder seeded with this client's settings,
// modify it, and Build again to get a reconfigured client:
//
//	newClient, err := client.Builder().SetTimeout(5 * time.Second).Build(ctx)
//
// ConfigurableClient embeds Client, so it can be used anywhere a plain Client
// is accepted. Code that doesn't need reconfiguration can ignore Builder entirely.
type ConfigurableClient[B ServiceBuilder[B]] interface {
	Client
	// Builder returns a new builder seeded with this client's configuration.
	// Modifications to the returned builder do not affect this client.
	Builder() B
}

// Middleware intercepts HTTP round-trips for cross-cutting concerns
// such as authentication, metrics, tracing, and error handling.
type Middleware interface {
	// RoundTrip executes the middleware logic, delegating to next for the actual request.
	RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error)
}

// MiddlewareFunc is a function adapter for the Middleware interface.
type MiddlewareFunc func(req *http.Request, next http.RoundTripper) (*http.Response, error)

// RoundTrip implements Middleware.
func (f MiddlewareFunc) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	return f(req, next)
}

// ErrorDecoder determines whether an HTTP response represents an error
// and decodes it into a Go error value.
type ErrorDecoder interface {
	// Handles returns true if this decoder should handle the given response.
	Handles(resp *http.Response) bool
	// DecodeError decodes the response into an error. Called only when Handles returns true.
	DecodeError(resp *http.Response) error
}

// TagsProvider produces metric tags from an HTTP request/response pair.
type TagsProvider interface {
	Tags(req *http.Request, resp *http.Response, err error) Tags
}

// Tags is a set of key-value metric tags. It implements TagsProvider as a
// convenience for static tags that don't depend on the request or response:
//
//	builder.SetMetrics(httpc.Tags{"service": "myapp", "env": "prod"})
type Tags map[string]string

// Tags implements TagsProvider by returning itself, ignoring the request, response, and error.
func (t Tags) Tags(*http.Request, *http.Response, error) Tags { return t }

// TagsProviderFunc is a function adapter for the TagsProvider interface.
type TagsProviderFunc func(req *http.Request, resp *http.Response, err error) Tags

// Tags implements TagsProvider.
func (f TagsProviderFunc) Tags(req *http.Request, resp *http.Response, err error) Tags {
	return f(req, resp, err)
}

// TokenProvider returns a bearer token for request authentication.
type TokenProvider func(ctx context.Context) (string, error)

// BasicAuth holds HTTP basic authentication credentials.
type BasicAuth struct {
	User     string
	Password string
}

// BasicAuthProvider returns basic auth credentials for request authentication.
type BasicAuthProvider func(ctx context.Context) (BasicAuth, error)

// URIScoringStrategy controls how base URIs are selected for requests.
type URIScoringStrategy int

const (
	// URIScoringBalanced scores URIs based on response latency and error rates,
	// preferring faster and more reliable hosts.
	URIScoringBalanced URIScoringStrategy = iota
	// URIScoringRandom selects URIs uniformly at random.
	URIScoringRandom
)
