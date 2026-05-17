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

	"github.com/palantir/pkg/metrics"
)

// Client is the transport interface returned by [Builder.Build]. Its signature
// matches *http.Client.Do, so a plain *http.Client satisfies it.
//
// A built Client prepends a selected base URL (per URI scoring) to the request
// path on each attempt, applies the middleware stack, enforces per-attempt
// timeouts, and retries replayable requests. Endpoint.Execute is the typical
// caller; it builds the request and decodes the response after Do returns.
type Client interface {
	Do(req *http.Request) (*http.Response, error)
}

// ConfigurableClient is a Client that exposes a fresh Builder seeded with its
// configuration, allowing reconfiguration without starting from scratch:
//
//	newClient, err := client.Builder().SetTimeout(5 * time.Second).Build(ctx)
type ConfigurableClient[B ServiceBuilder[B]] interface {
	Client
	Builder() B
}

// Middleware wraps HTTP round-trips for cross-cutting concerns such as
// authentication, metrics, tracing, and error handling.
type Middleware interface {
	RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error)
}

// MiddlewareFunc adapts a function to Middleware.
type MiddlewareFunc func(req *http.Request, next http.RoundTripper) (*http.Response, error)

func (f MiddlewareFunc) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	return f(req, next)
}

// TagsProvider produces metric tags from an HTTP request/response pair.
type TagsProvider interface {
	Tags(req *http.Request, resp *http.Response, err error) metrics.Tags
}

// TagsProviderFunc adapts a function to TagsProvider.
type TagsProviderFunc func(req *http.Request, resp *http.Response, err error) metrics.Tags

func (f TagsProviderFunc) Tags(req *http.Request, resp *http.Response, err error) metrics.Tags {
	return f(req, resp, err)
}

// StaticTagsProvider attaches the same tags to every request.
type StaticTagsProvider metrics.Tags

func (s StaticTagsProvider) Tags(req *http.Request, resp *http.Response, err error) metrics.Tags {
	return metrics.Tags(s)
}

// Void is the Req or Resp type for endpoints with no request or response body.
type Void = struct{}

// TokenProvider returns a bearer token for request authentication.
type TokenProvider func(ctx context.Context) (string, error)

// BasicAuthProvider returns basic auth credentials for request authentication.
type BasicAuthProvider func(ctx context.Context) (BasicAuth, error)

// URIScoringStrategy controls how base URIs are selected for requests.
type URIScoringStrategy int

const (
	// URIScoringBalanced prefers faster, more reliable hosts based on observed latency and error rates.
	URIScoringBalanced URIScoringStrategy = iota
	// URIScoringRandom selects URIs uniformly at random.
	URIScoringRandom
)
