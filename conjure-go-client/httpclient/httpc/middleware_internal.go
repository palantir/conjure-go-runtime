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

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient/internal"
	"github.com/palantir/pkg/refreshable/v2"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/palantir/witchcraft-go-tracing/wtracing"
	"github.com/palantir/witchcraft-go-tracing/wtracing/propagation/b3"
)

// wrapTransport composes middleware around a base http.RoundTripper.
// Each middleware wraps the previous, creating a chain.
// nil middleware values are skipped.
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

// recoveryMiddleware recovers panics during the request and returns them as errors.
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

// errorDecoderMiddleware intercepts responses and returns decoded errors.
// When the error decoder handles a response, the response body is drained
// and closed before returning the decoded error.
type errorDecoderMiddleware struct {
	decoder ErrorDecoder
}

func (e errorDecoderMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	resp, err := next.RoundTrip(req)
	// If error is already set, it is more severe than our HTTP error. Just return it.
	if resp == nil || err != nil {
		return nil, err
	}
	if e.decoder.Handles(resp) {
		defer internal.DrainBody(req.Context(), resp)
		return nil, e.decoder.DecodeError(resp)
	}
	return resp, nil
}

// traceMiddleware injects tracing information into request headers.
type traceMiddleware struct {
	serviceName         refreshable.Refreshable[string]
	disableRequestSpan  bool
	disableTraceHeaders bool
}

const traceIDHeaderKey = "X-B3-TraceId"

func (t *traceMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
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
	}

	return next.RoundTrip(req)
}

// middlewareChain applies a slice of middleware around a base RoundTripper
// using iterative delegation rather than nested wrapping. This produces
// flatter stack traces compared to deeply nested wrappedTransport structs.
// nil middleware values in the slice are skipped.
type middlewareChain struct {
	middlewares []Middleware
	base        http.RoundTripper
}

func (c *middlewareChain) RoundTrip(req *http.Request) (*http.Response, error) {
	// Build the chain from innermost (last) to outermost (first).
	rt := c.base
	for i := len(c.middlewares) - 1; i >= 0; i-- {
		if c.middlewares[i] == nil {
			continue
		}
		mw := c.middlewares[i]
		next := rt
		rt = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			return mw.RoundTrip(r, next)
		})
	}
	return rt.RoundTrip(req)
}
