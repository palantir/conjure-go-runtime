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
	"net/http"
	"net/url"

	"github.com/palantir/pkg/bytesbuffers"
	"github.com/palantir/witchcraft-go-error"
	"github.com/palantir/witchcraft-go-tracing/wtracing"
)

type requestBuilder struct {
	method         string
	path           string
	address        string
	headers        http.Header
	query          url.Values
	bodyMiddleware *bodyMiddleware
	bufferPool     bytesbuffers.Pool

	middlewares  []Middleware
	configureCtx []func(context.Context) context.Context
}

const traceIDHeaderKey = "X-B3-TraceId"

type RequestParam interface {
	apply(*requestBuilder) error
}

type requestParamFunc func(*requestBuilder) error

func (f requestParamFunc) apply(b *requestBuilder) error {
	return f(b)
}

// NewRequest returns an *http.Request and a set of Middlewares which should be
// wrapped around the request during execution.
func (c *clientImpl) newRequest(ctx context.Context, baseURL string, params ...RequestParam) (*http.Request, []Middleware, error) {
	b := &requestBuilder{
		headers:        c.initializeRequestHeaders(ctx),
		query:          make(url.Values),
		bodyMiddleware: &bodyMiddleware{bufferPool: c.bufferPool},
	}

	for _, p := range params {
		if p == nil {
			continue
		}
		if err := p.apply(b); err != nil {
			return nil, nil, err
		}
	}
	for _, c := range b.configureCtx {
		ctx = c(ctx)
	}

	if b.method == "" {
		return nil, nil, werror.Error("httpclient: use WithRequestMethod() to specify HTTP method")
	}

	req, err := http.NewRequest(b.method, baseURL+b.path, nil)
	if err != nil {
		return nil, nil, werror.Wrap(err, "failed to build new HTTP request")
	}
	req = req.WithContext(ctx)
	req.Header = b.headers
	if q := b.query.Encode(); q != "" {
		req.URL.RawQuery = q
	}

	if b.bodyMiddleware != nil {
		b.middlewares = append(b.middlewares, b.bodyMiddleware)
	}

	return req, b.middlewares, nil
}

func (c *clientImpl) initializeRequestHeaders(ctx context.Context) http.Header {
	headers := make(http.Header)
	if !c.disableTraceHeaderPropagation {
		traceID := wtracing.TraceIDFromContext(ctx)
		if traceID != "" {
			headers.Set(traceIDHeaderKey, string(traceID))
		}
	}
	return headers
}
