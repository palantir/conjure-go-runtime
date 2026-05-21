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
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"time"

	"github.com/palantir/pkg/metrics"
	"github.com/palantir/pkg/refreshable/v2"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/palantir/witchcraft-go-logging/wlog/svclog/svc1log"
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

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type clientFunc func(*http.Request) (*http.Response, error)

func (f clientFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

func wrapClientMiddleware(c Client, mw Middleware) Client {
	return clientFunc(func(req *http.Request) (*http.Response, error) {
		return mw.RoundTrip(req, roundTripperFunc(c.Do))
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

// recoveryMiddleware converts panics into errors.
type recoveryMiddleware struct{}

func (recoveryMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (resp *http.Response, err error) {
	defer func() {
		if r := recover(); r != nil {
			if err == nil {
				err = werror.ErrorWithContextParams(req.Context(), "recovered panic", werror.UnsafeParam("recovered", fmt.Sprintf("%v", r)))
			} else {
				err = werror.WrapWithContextParams(req.Context(), err, "recovered panic", werror.UnsafeParam("recovered", fmt.Sprintf("%v", r)))
			}
		}
	}()
	return next.RoundTrip(req)
}

// telemetryMiddleware increments metrics, starts a per-request span, and propagates B3 trace headers.
type telemetryMiddleware struct {
	serviceName         refreshable.Refreshable[string]
	tags                []TagsProvider
	disableMetrics      refreshable.Refreshable[bool]
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
		const (
			metricClientResponse  = "client.response"          // Timer; full round-trip; +method, method-name, family
			metricRequestInFlight = "client.request.in-flight" // Counter; concurrent requests
		)
		serviceNameTag := metrics.NewTagWithFallbackValue(metricTagServiceName, t.serviceName.Current(), "unknown")
		registry := metrics.FromContext(metrics.AddTags(ctx, serviceNameTag))
		if !t.disableTraceMetrics {
			req = req.WithContext(httptrace.WithClientTrace(ctx, newMetricsClientTrace(registry, svc1log.FromContext(ctx))))
		}
		registry.Counter(metricRequestInFlight).Inc(1)
		start := time.Now()
		defer func() {
			registry.Counter(metricRequestInFlight).Dec(1)
			registry.Timer(metricClientResponse, responseMetricTags(t.tags, req, resp, err)...).UpdateSince(start)
		}()
	}

	return next.RoundTrip(req)
}

// drainBody is a best-effort drain-and-close used to free connections when a
// response will be discarded.
func drainBody(ctx context.Context, resp *http.Response) {
	if resp != nil && resp.Body != nil {
		if bytes, err := io.Copy(io.Discard, resp.Body); err != nil {
			svc1log.FromContext(ctx).Warn("Failed to drain entire response body",
				svc1log.SafeParam("bytes", bytes),
				svc1log.Stacktrace(err))
		} else if bytes > 0 {
			svc1log.FromContext(ctx).Debug("Drained remaining response body",
				svc1log.SafeParam("bytes", bytes))
		}
		if err := resp.Body.Close(); err != nil {
			svc1log.FromContext(ctx).Warn("Failed to close response body",
				svc1log.Stacktrace(err))
		}
	}
}
