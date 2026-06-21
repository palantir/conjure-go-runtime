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

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/internal"
	"github.com/palantir/pkg/bytesbuffers"
	werror "github.com/palantir/witchcraft-go-error"
)

// A Client executes requests to a configured service.
//
// The Get/Head/Post/Put/Delete methods are for conveniently setting the method type and calling Do()
type Client interface {
	// Do executes a full request. Any input or output should be specified via params.
	// By the time it is returned, the response's body will be fully read and closed.
	// Use the WithResponse* params to unmarshal the body before Do() returns.
	//
	// In the case of a response with StatusCode >= 400, Do() will return a nil response and a non-nil error.
	// Use StatusCodeFromError(err) to retrieve the code from the error.
	// Use WithDisableRestErrors() to disable this middleware on your client.
	// Use WithErrorDecoder(errorDecoder) to replace this default behavior with custom error decoding behavior.
	Do(ctx context.Context, params ...RequestParam) (*http.Response, error)

	Get(ctx context.Context, params ...RequestParam) (*http.Response, error)
	Head(ctx context.Context, params ...RequestParam) (*http.Response, error)
	Post(ctx context.Context, params ...RequestParam) (*http.Response, error)
	Put(ctx context.Context, params ...RequestParam) (*http.Response, error)
	Delete(ctx context.Context, params ...RequestParam) (*http.Response, error)
}

type clientImpl struct {
	client       httpc.Runtime
	errorDecoder ErrorDecoder
	bufferPool   bytesbuffers.Pool
}

func (c *clientImpl) Get(ctx context.Context, params ...RequestParam) (*http.Response, error) {
	return c.Do(ctx, append(params, WithRequestMethod(http.MethodGet))...)
}

func (c *clientImpl) Head(ctx context.Context, params ...RequestParam) (*http.Response, error) {
	return c.Do(ctx, append(params, WithRequestMethod(http.MethodHead))...)
}

func (c *clientImpl) Post(ctx context.Context, params ...RequestParam) (*http.Response, error) {
	return c.Do(ctx, append(params, WithRequestMethod(http.MethodPost))...)
}

func (c *clientImpl) Put(ctx context.Context, params ...RequestParam) (*http.Response, error) {
	return c.Do(ctx, append(params, WithRequestMethod(http.MethodPut))...)
}

func (c *clientImpl) Delete(ctx context.Context, params ...RequestParam) (*http.Response, error) {
	return c.Do(ctx, append(params, WithRequestMethod(http.MethodDelete))...)
}

func (c *clientImpl) Do(ctx context.Context, params ...RequestParam) (*http.Response, error) {
	b := &requestBuilder{
		headers:        make(http.Header),
		query:          make(url.Values),
		bodyMiddleware: &bodyMiddleware{bufferPool: c.bufferPool},
	}
	for _, p := range params {
		if p == nil {
			continue
		}
		if err := p.apply(b); err != nil {
			return nil, err
		}
	}

	for _, cfg := range b.configureCtx {
		ctx = cfg(ctx)
	}

	if b.method == "" {
		return nil, werror.ErrorWithContextParams(ctx, "httpclient: use WithRequestMethod() to specify HTTP method")
	}

	// Path-only URL; httpc prepends the base URI on each attempt.
	req, err := http.NewRequestWithContext(ctx, b.method, b.path, nil)
	if err != nil {
		return nil, werror.WrapWithContextParams(ctx, err, "failed to build new HTTP request")
	}
	req.Header = b.headers

	cleanup, err := b.bodyMiddleware.setRequestBody(req)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	// Carry request headers and query as SendOptions.Values rather than mutating req
	// directly, so they resolve per attempt ABOVE the client's intrinsic auth and
	// headers — a request Authorization (e.g. WithRequestBasicAuth) and request
	// headers/query take precedence over client-scoped values, as documented. Body
	// headers (Content-Type) that setRequestBody put on req.Header travel too.
	var values httpc.RequestValues
	for key, vs := range req.Header {
		if len(vs) == 0 {
			continue
		}
		values = values.WithHeader(key, vs[0], vs[1:]...)
	}
	for key, vs := range b.query {
		if len(vs) == 0 {
			continue
		}
		values = values.WithQuery(key, vs[0], vs[1:]...)
	}
	req.Header = make(http.Header)

	opts := httpc.SendOptions{Values: values}
	if b.requestTimeout != nil {
		opts.Policy = opts.Policy.WithTimeout(*b.requestTimeout)
	}

	resp, respErr := c.client.Send(ctx, req, opts)

	// Error decoding: per-request first, then client-level fallback.
	if respErr == nil && resp != nil {
		var ed ErrorDecoder
		if b.errorDecoderMiddleware != nil && b.errorDecoderMiddleware.Handles(resp) {
			ed = b.errorDecoderMiddleware
		} else if c.errorDecoder != nil && c.errorDecoder.Handles(resp) {
			ed = c.errorDecoder
		}
		if ed != nil {
			respErr = ed.DecodeError(resp)
			internal.DrainBody(ctx, resp)
		}
	}

	// Decode response body before draining so the body is still readable.
	readErr := b.bodyMiddleware.readResponse(resp, respErr)

	if !(respErr == nil && b.bodyMiddleware.rawOutput) {
		internal.DrainBody(ctx, resp)
	}

	if readErr != nil {
		return nil, readErr
	}
	if respErr != nil {
		return nil, respErr
	}
	return resp, nil
}
