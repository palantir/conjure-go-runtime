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

// requestMiddlewareApplier applies the per-request middlewares carried on the
// request context (set by [Endpoint.Execute]) at this point in the baked stack.
// It is baked just inside telemetry so per-request middlewares run on each
// attempt with the resolved URL, are traced/metered/recovered, and run after
// telemetry's header injection — so their changes are not overwritten, matching
// the builder middleware.
type requestMiddlewareApplier struct{}

func (requestMiddlewareApplier) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	middlewares := requestMiddlewaresFromContext(req.Context())
	if len(middlewares) == 0 {
		return next.RoundTrip(req)
	}
	return wrapTransport(next, middlewares...).RoundTrip(req)
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
		metricsReq, callback := NewMetricsResponseCallback(req, t.serviceName.Current(), t.disableTraceMetrics, t.tags...)
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
