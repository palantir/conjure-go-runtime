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
	"fmt"
	"net/http"

	"github.com/palantir/pkg/refreshable/v2"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/palantir/witchcraft-go-tracing/wtracing"
	"github.com/palantir/witchcraft-go-tracing/wtracing/propagation/b3"
)

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

// composeMiddleware collapses a list of middlewares into a single [Middleware]
// that wraps next with each in turn — the last in the list is outermost, matching
// [wrapTransport]. Returns nil when the list has no non-nil entries.
func composeMiddleware(middlewares ...Middleware) Middleware {
	var nonNil []Middleware
	for _, mw := range middlewares {
		if mw != nil {
			nonNil = append(nonNil, mw)
		}
	}
	if len(nonNil) == 0 {
		return nil
	}
	return MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		return wrapTransport(next, nonNil...).RoundTrip(req)
	})
}

// wrapTransport composes middlewares around base. Each successive middleware
// wraps the previous, so the last in the list is outermost. Nil entries are skipped.
func wrapTransport(base http.RoundTripper, middlewares ...Middleware) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	for _, mw := range middlewares {
		if mw != nil {
			base = &wrappedTransport{base: base, middleware: mw}
		}
	}
	return base
}

type wrappedTransport struct {
	base       http.RoundTripper
	middleware Middleware
}

func (w *wrappedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return w.middleware.RoundTrip(req, w.base)
}

// CloseIdleConnections forwards the standard optional capability down the wrapper
// chain so a *http.Client built from a wrapped transport can still release idle
// connections on the underlying *http.Transport.
func (w *wrappedTransport) CloseIdleConnections() {
	closeIdleConnections(w.base)
}

// closeIdleConnections invokes the optional CloseIdleConnections capability on rt
// if it implements it, matching net/http.Client.CloseIdleConnections's behavior.
func closeIdleConnections(rt http.RoundTripper) {
	if c, ok := rt.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
}

// telemetryMiddleware increments metrics, starts a per-request span, and propagates B3 trace headers.
type telemetryMiddleware struct {
	serviceName         refreshable.Refreshable[string]
	tags                []TagsProvider
	disableMetrics      refreshable.Refreshable[bool]
	disableRecovery     bool
	disableRequestSpan  bool
	disableTraceHeaders bool
	disableTraceMetrics bool
}

const (
	traceIDHeaderKey      = "X-B3-TraceId"
	forUserAgentHeaderKey = "For-User-Agent"
)

func (t *telemetryMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (resp *http.Response, err error) {
	ctx := req.Context()
	span := wtracing.SpanFromContext(ctx)

	if !t.disableRequestSpan {
		if method, ok := RPCMethodName(ctx); ok && method != "" {
			span, ctx = wtracing.StartSpanFromContext(ctx, wtracing.TracerFromContext(ctx), method,
				wtracing.WithKind(wtracing.Client),
				wtracing.WithRemoteEndpoint(&wtracing.Endpoint{ServiceName: t.serviceName.Current()}))
			if span != nil {
				defer span.Finish()
			}
			req = req.WithContext(ctx)
		}
	}

	if !t.disableTraceHeaders {
		if span != nil {
			b3.SpanInjector(req)(span.Context())
		} else if traceID := wtracing.TraceIDFromContext(ctx); traceID != "" {
			req.Header.Set(traceIDHeaderKey, string(traceID))
		}
		if forUserAgent, ok := forUserAgentFromContext(ctx); ok && forUserAgent != "" && req.Header.Get(forUserAgentHeaderKey) == "" {
			req.Header.Set(forUserAgentHeaderKey, forUserAgent)
		}
	}

	if t.disableMetrics == nil || !t.disableMetrics.Current() {
		metricsReq, callback := newMetricsResponseCallback(req, t.serviceName.Current(), t.disableTraceMetrics, t.tags...)
		defer func() {
			callback(resp, err)
		}()
		req = metricsReq
	}

	if !t.disableRecovery {
		defer func() {
			if r := recover(); r != nil {
				if err == nil {
					err = werror.ErrorWithContextParams(req.Context(), "recovered panic", werror.UnsafeParam("recovered", fmt.Sprintf("%v", r)))
				} else {
					err = werror.WrapWithContextParams(req.Context(), err, "recovered panic", werror.UnsafeParam("recovered", fmt.Sprintf("%v", r)))
				}
			}
		}()
	}

	return next.RoundTrip(req)
}
