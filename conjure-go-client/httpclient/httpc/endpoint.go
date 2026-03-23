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
	"net/http"
	"net/url"
	"strings"
	"time"
)

// rpcMethodNameKey is the context key for the RPC method name set by Endpoint.Execute.
type rpcMethodNameKey struct{}

// forUserAgentKey is the context key for the For-User-Agent header value.
type forUserAgentKey struct{}

// requestTimeoutKey is the context key for per-request timeout overrides.
type requestTimeoutKey struct{}

// RPCMethodName extracts the RPC method name from the context, if set by Endpoint.Execute.
func RPCMethodName(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(rpcMethodNameKey{}).(string)
	return v, ok
}

// ContextWithRPCMethodName returns a new context with the RPC method name set for use in logging and metrics.
func ContextWithRPCMethodName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, rpcMethodNameKey{}, name)
}

// ContextWithForUserAgent returns a new context with the For-User-Agent header value set.
func ContextWithForUserAgent(ctx context.Context, forUserAgent string) context.Context {
	if forUserAgent == "" {
		return ctx
	}
	return context.WithValue(ctx, forUserAgentKey{}, forUserAgent)
}

func forUserAgentFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(forUserAgentKey{}).(string)
	return v, ok
}

// ContextWithRequestTimeout returns a new context carrying a per-request timeout
// that overrides the client-level timeout for a single request attempt.
func ContextWithRequestTimeout(ctx context.Context, d time.Duration) context.Context {
	return context.WithValue(ctx, requestTimeoutKey{}, d)
}

// requestTimeoutFromContext extracts a per-request timeout from the context, if set.
func requestTimeoutFromContext(ctx context.Context) (time.Duration, bool) {
	v, ok := ctx.Value(requestTimeoutKey{}).(time.Duration)
	return v, ok
}

// roundTripperFunc adapts a function to http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// wrapClientMiddleware wraps a Client with a Middleware.
func wrapClientMiddleware(c Client, mw Middleware) Client {
	return clientFunc(func(req *http.Request) (*http.Response, error) {
		return mw.RoundTrip(req, roundTripperFunc(c.Do))
	})
}

type clientFunc func(*http.Request) (*http.Response, error)

func (f clientFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

// RequestOverrides defines per-request configuration methods shared by Endpoint and
// Overrides. Unlike builder interfaces, all methods use copy-on-write semantics:
// they return a new value with the override applied, leaving the original unchanged.
// This makes it safe to derive multiple specialized configurations from a shared base:
//
//	base := ep.AddHeader("X-Tenant", "acme")
//	v1 := base.AddHeader("Api-Version", "1")
//	v2 := base.AddHeader("Api-Version", "2") // base and v1 are unaffected
//
// The type parameter D is the concrete implementing type (F-bounded polymorphism),
// ensuring that methods on Endpoint return Endpoint and methods on Overrides
// return Overrides.
type RequestOverrides[D any] interface {
	// AddHeader adds a request header. Multiple calls with the same key accumulate values.
	AddHeader(key, value string) D
	// SetHeader sets a request header, replacing any previously added or set values for the key.
	SetHeader(key, value string) D
	// AddQuery adds a query parameter. Multiple calls with the same key accumulate values.
	AddQuery(key, value string) D
	// SetQuery sets a query parameter, replacing any previously added or set values for the key.
	SetQuery(key, value string) D
	// WithTimeout sets a per-request timeout that overrides the client-level timeout.
	WithTimeout(time.Duration) D
	// WithErrorDecoder sets a per-request error decoder that overrides the client-level decoder.
	WithErrorDecoder(ErrorDecoder) D
	// WithBasicAuth sets per-request basic auth credentials, overriding any client-level auth.
	WithBasicAuth(user, password string) D
	// WithMiddleware appends a per-request middleware to the chain.
	WithMiddleware(Middleware) D
}

// Endpoint is a copy-on-write request descriptor that pairs an HTTP method and path
// with a typed encoder and decoder. All methods (SetEncoder, SetDecoder, SetAccept,
// and the RequestOverrides methods) return a new Endpoint value without modifying
// the original.
//
// Endpoint is safe for concurrent use: multiple goroutines may call methods on
// the same Endpoint value simultaneously, and each receives an independent copy.
// This makes Endpoint safe to store as a package-level base configuration
// and derive per-call variants from it concurrently:
//
//	// Package-level base endpoint.
//	var createItem = httpc.NewEndpoint[CreateReq, CreateResp](http.MethodPost, "/api/v1/items", "CreateItem").
//	    SetEncoder(httpc.JSONEncoder[CreateReq]()).
//	    SetDecoder(httpc.JSONDecoder[CreateResp]()).
//	    SetAccept("application/json")
//
//	// Per-call customization (does not modify createItem).
//	resp, _, err := createItem.AddHeader("Idempotency-Key", key).Execute(ctx, client, req)
//
// Additional examples:
//
//	// No-body GET with JSON response
//	ep := httpc.NewEndpoint[struct{}, MyResp](http.MethodGet, "/api/v1/item", "GetItem").
//	    SetDecoder(httpc.JSONDecoder[MyResp]()).
//	    SetAccept("application/json")
//
//	// Void DELETE (no Accept header needed)
//	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodDelete, "/api/v1/item", "Delete").
//	    SetDecoder(httpc.VoidDecoder())
//
//	// Binary download
//	ep := httpc.NewEndpoint[struct{}, io.ReadCloser](http.MethodGet, "/dl", "Download").
//	    SetDecoder(httpc.BinaryDecoder()).
//	    SetAccept("application/octet-stream")
type Endpoint[Req, Resp any] struct {
	method    string
	path      string
	name      string
	accept    string
	overrides Overrides
	encoder   BodyEncoder[Req]
	decoder   BodyDecoder[Resp]
}

// NewEndpoint creates a new Endpoint with the given HTTP method, path, and RPC name.
// The RPC name is used in tracing spans and metrics tags.
// Use SetEncoder and SetDecoder to configure body serialization before calling Execute.
func NewEndpoint[Req, Resp any](method, path, name string) Endpoint[Req, Resp] {
	return Endpoint[Req, Resp]{
		method: method,
		path:   path,
		name:   name,
	}
}

// NewGET creates a GET endpoint with no request body.
func NewGET[Resp any](path, name string) Endpoint[struct{}, Resp] {
	return NewEndpoint[struct{}, Resp](http.MethodGet, path, name)
}

// NewDELETE creates a DELETE endpoint with no request body.
func NewDELETE[Resp any](path, name string) Endpoint[struct{}, Resp] {
	return NewEndpoint[struct{}, Resp](http.MethodDelete, path, name)
}

// NewHEAD creates a HEAD endpoint with no request body.
func NewHEAD[Resp any](path, name string) Endpoint[struct{}, Resp] {
	return NewEndpoint[struct{}, Resp](http.MethodHead, path, name)
}

// NewPOST creates a POST endpoint with a typed request body.
func NewPOST[Req, Resp any](path, name string) Endpoint[Req, Resp] {
	return NewEndpoint[Req, Resp](http.MethodPost, path, name)
}

// NewPUT creates a PUT endpoint with a typed request body.
func NewPUT[Req, Resp any](path, name string) Endpoint[Req, Resp] {
	return NewEndpoint[Req, Resp](http.MethodPut, path, name)
}

// NewPATCH creates a PATCH endpoint with a typed request body.
func NewPATCH[Req, Resp any](path, name string) Endpoint[Req, Resp] {
	return NewEndpoint[Req, Resp](http.MethodPatch, path, name)
}

// NewJSONGET creates a GET endpoint pre-configured with a JSON decoder and Accept header.
// This is a convenience for the common case of a GET returning a JSON response:
//
//	result, _, err := httpc.ExecuteVoid(ctx, client,
//	    httpc.NewJSONGET[MyResp]("/items/123", "GetItem"))
func NewJSONGET[Resp any](path, name string) Endpoint[struct{}, Resp] {
	return NewGET[Resp](path, name).
		SetDecoder(JSONDecoder[Resp]()).
		SetAccept("application/json")
}

// NewJSONPOST creates a POST endpoint pre-configured with a JSON encoder, JSON decoder,
// and Accept header. This is a convenience for the common case of a JSON request/response POST:
//
//	result, _, err := httpc.NewJSONPOST[CreateReq, CreateResp]("/items", "CreateItem").
//	    Execute(ctx, client, body)
func NewJSONPOST[Req, Resp any](path, name string) Endpoint[Req, Resp] {
	return NewPOST[Req, Resp](path, name).
		SetEncoder(JSONEncoder[Req]()).
		SetDecoder(JSONDecoder[Resp]()).
		SetAccept("application/json")
}

// NewJSONPUT creates a PUT endpoint pre-configured with a JSON encoder, JSON decoder,
// and Accept header.
func NewJSONPUT[Req, Resp any](path, name string) Endpoint[Req, Resp] {
	return NewPUT[Req, Resp](path, name).
		SetEncoder(JSONEncoder[Req]()).
		SetDecoder(JSONDecoder[Resp]()).
		SetAccept("application/json")
}

// SetEncoder sets the body encoder for the request. Returns a new Endpoint value.
func (e Endpoint[Req, Resp]) SetEncoder(enc BodyEncoder[Req]) Endpoint[Req, Resp] {
	e.encoder = enc
	return e
}

// SetDecoder sets the body decoder for the response. Returns a new Endpoint value.
func (e Endpoint[Req, Resp]) SetDecoder(dec BodyDecoder[Resp]) Endpoint[Req, Resp] {
	e.decoder = dec
	return e
}

// SetAccept sets the Accept header value sent with the request. Returns a new Endpoint value.
// Pass "" to send no Accept header (the default). The Accept header can still be
// overridden per-request via SetHeader("Accept", ...).
func (e Endpoint[Req, Resp]) SetAccept(accept string) Endpoint[Req, Resp] {
	e.accept = accept
	return e
}

// WithPathParam replaces a named {key} placeholder in the endpoint's path template
// with url.PathEscape(fmt.Sprint(value)). The path is stored as a Conjure-style
// template (e.g. "/items/{itemId}/version/{version}") and each call fills in one
// parameter by name, so arguments may be provided in any order:
//
//	var ep = httpc.NewGET[Resp]("/items/{itemId}/version/{version}", "GetItem")
//
//	// These two are equivalent:
//	ep.WithPathParam("itemId", id).WithPathParam("version", v)
//	ep.WithPathParam("version", v).WithPathParam("itemId", id)
//
// Trailing (greedy) parameters are also supported. A placeholder ending in *
// (e.g. {filePath*}) preserves slashes in the value while still escaping each
// individual path segment:
//
//	var ep = httpc.NewGET[Resp]("/files/{filePath*}", "GetFile")
//	ep.WithPathParam("filePath", "dir/sub dir/file.txt")
//	// produces path: /files/dir/sub%20dir/file.txt
//
// Returns a new Endpoint value; the original is unchanged.
func (e Endpoint[Req, Resp]) WithPathParam(key string, value any) Endpoint[Req, Resp] {
	s := fmt.Sprint(value)
	// Check for greedy placeholder {key*} first — preserves slashes.
	if glob := "{" + key + "*}"; strings.Contains(e.path, glob) {
		segments := strings.Split(s, "/")
		for i, seg := range segments {
			segments[i] = url.PathEscape(seg)
		}
		e.path = strings.ReplaceAll(e.path, glob, strings.Join(segments, "/"))
		return e
	}
	e.path = strings.ReplaceAll(e.path, "{"+key+"}", url.PathEscape(s))
	return e
}

// AddHeader adds a header to the request. Multiple calls with the same key accumulate values.
// Returns a new Endpoint value.
func (e Endpoint[Req, Resp]) AddHeader(key, value string) Endpoint[Req, Resp] {
	e.overrides = e.overrides.AddHeader(key, value)
	return e
}

// SetHeader sets a header on the request, replacing any previously added or set values for the key.
// Returns a new Endpoint value.
func (e Endpoint[Req, Resp]) SetHeader(key, value string) Endpoint[Req, Resp] {
	e.overrides = e.overrides.SetHeader(key, value)
	return e
}

// AddQuery adds a query parameter to the request. Multiple calls with the same key accumulate values.
// Returns a new Endpoint value.
func (e Endpoint[Req, Resp]) AddQuery(key, value string) Endpoint[Req, Resp] {
	e.overrides = e.overrides.AddQuery(key, value)
	return e
}

// SetQuery sets a query parameter on the request, replacing any previously added or set values for the key.
// Returns a new Endpoint value.
func (e Endpoint[Req, Resp]) SetQuery(key, value string) Endpoint[Req, Resp] {
	e.overrides = e.overrides.SetQuery(key, value)
	return e
}

// WithTimeout sets a per-request timeout override. Returns a new Endpoint value.
func (e Endpoint[Req, Resp]) WithTimeout(d time.Duration) Endpoint[Req, Resp] {
	e.overrides = e.overrides.WithTimeout(d)
	return e
}

// WithErrorDecoder sets a per-request error decoder override. Returns a new Endpoint value.
func (e Endpoint[Req, Resp]) WithErrorDecoder(d ErrorDecoder) Endpoint[Req, Resp] {
	e.overrides = e.overrides.WithErrorDecoder(d)
	return e
}

// WithBasicAuth sets per-request basic auth credentials. Returns a new Endpoint value.
func (e Endpoint[Req, Resp]) WithBasicAuth(user, password string) Endpoint[Req, Resp] {
	e.overrides = e.overrides.WithBasicAuth(user, password)
	return e
}

// WithMiddleware appends a per-request middleware. Returns a new Endpoint value.
func (e Endpoint[Req, Resp]) WithMiddleware(m Middleware) Endpoint[Req, Resp] {
	e.overrides = e.overrides.WithMiddleware(m)
	return e
}

// WithOverrides merges the given Overrides into the endpoint. Set headers/query replace
// matching keys and clear adds; add headers/query accumulate. Timeout, error decoder,
// and basic auth use last-wins; middlewares are appended.
// Returns a new Endpoint value; the original is unchanged.
func (e Endpoint[Req, Resp]) WithOverrides(o Overrides) Endpoint[Req, Resp] {
	e.overrides = e.overrides.merge(o)
	return e
}

// Execute performs the HTTP request using the given client and request body.
// It builds an *http.Request from the endpoint's method, path, headers, query parameters,
// and encoded body, then delegates to client.Do. The response is decoded using the
// configured BodyDecoder. Per-request overrides (timeout, error decoder, basic auth,
// middleware) are applied on top of the client's defaults.
//
// The returned *http.Response is the raw HTTP response with its body already consumed
// (drained or decoded). It is useful for inspecting response headers, status codes,
// and trailers. On error, the *http.Response may be nil (e.g. transport errors) or
// non-nil (e.g. error decoder errors where the response was received).
func (e Endpoint[Req, Resp]) Execute(ctx context.Context, client Client, body Req) (Resp, *http.Response, error) {
	var zero Resp

	// Verify all path template parameters have been filled in.
	if i := strings.IndexByte(e.path, '{'); i != -1 {
		if j := strings.IndexByte(e.path[i:], '}'); j != -1 {
			param := e.path[i : i+j+1]
			return zero, nil, fmt.Errorf("httpc: path parameter %s not populated in %s %s", param, e.method, e.path)
		}
	}

	// Store RPC method name on context for tracing/metrics middleware.
	if e.name != "" {
		ctx = context.WithValue(ctx, rpcMethodNameKey{}, e.name)
	}

	// Inject buffer pool from client into context for use by encoders/decoders.
	if pp, ok := client.(poolProvider); ok {
		if pool := pp.getBufferPool(); pool != nil {
			ctx = context.WithValue(ctx, bufferPoolKey{}, pool)
		}
	}

	// Build request with path-only URL; Client prepends base URI.
	req, err := http.NewRequestWithContext(ctx, e.method, e.path, nil)
	if err != nil {
		return zero, nil, err
	}

	// Encode body (sets Content-Type, Body, GetBody, ContentLength).
	if e.encoder != nil {
		if err := e.encoder.Encode(req, body); err != nil {
			return zero, nil, err
		}
	}

	// Set Accept header.
	if e.accept != "" {
		req.Header.Set("Accept", e.accept)
	}

	// Apply set headers first (replaces, including Accept/Content-Type).
	for k, vs := range e.overrides.setHeaders {
		req.Header[k] = append([]string(nil), vs...)
	}
	// Apply add headers (accumulates on top).
	for k, vs := range e.overrides.addHeaders {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	// Build query from set + add.
	if len(e.overrides.setQuery) > 0 || len(e.overrides.addQuery) > 0 {
		q := make(url.Values)
		for k, vs := range e.overrides.setQuery {
			q[k] = append([]string(nil), vs...)
		}
		for k, vs := range e.overrides.addQuery {
			for _, v := range vs {
				q.Add(k, v)
			}
		}
		req.URL.RawQuery = q.Encode()
	}

	// Apply basic auth.
	if e.overrides.basicAuth != nil {
		req.SetBasicAuth(e.overrides.basicAuth.user, e.overrides.basicAuth.password)
	}

	// Apply timeout via context.
	if e.overrides.timeout != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *e.overrides.timeout)
		defer cancel()
		req = req.WithContext(ctx)
	}

	// Wrap client with per-endpoint middleware (last added is outermost).
	c := client
	for _, mw := range e.overrides.middlewares {
		if mw != nil {
			c = wrapClientMiddleware(c, mw)
		}
	}

	// Execute.
	resp, err := c.Do(req)
	if err != nil {
		return zero, nil, err
	}

	// Per-endpoint error decoding (before response decode).
	if e.overrides.errorDecoder != nil && e.overrides.errorDecoder.Handles(resp) {
		drainBody(ctx, resp)
		return zero, resp, e.overrides.errorDecoder.DecodeError(resp)
	}

	// Decode response.
	if e.decoder != nil {
		result, err := e.decoder.Decode(ctx, resp)
		if err != nil {
			drainBody(ctx, resp)
			return zero, resp, err
		}
		// Skip draining for decoders that return the body directly to the caller.
		if _, raw := e.decoder.(rawBodyDecoder); !raw {
			drainBody(ctx, resp)
		}
		return result, resp, nil
	}
	// No decoder: drain body.
	drainBody(ctx, resp)
	return zero, resp, nil
}

// ExecuteVoid is a convenience wrapper for executing endpoints with no request body
// (Req = struct{}), avoiding the need to pass an explicit struct{}{} at every call site.
func ExecuteVoid[Resp any](ctx context.Context, client Client, ep Endpoint[struct{}, Resp]) (Resp, *http.Response, error) {
	return ep.Execute(ctx, client, struct{}{})
}

// WithTraceHeader sets the X-B3-TraceId header on any RequestOverrides value.
// It works with both Endpoint and Overrides implementations.
func WithTraceHeader[D RequestOverrides[D]](d D, traceID string) D {
	return d.SetHeader("X-B3-TraceId", traceID)
}

// WithStandardHeaders sets all key-value pairs from headers on any RequestOverrides value.
// Each header is set (not added), so calling this multiple times replaces previous values.
func WithStandardHeaders[D RequestOverrides[D]](d D, headers map[string]string) D {
	for k, v := range headers {
		d = d.SetHeader(k, v)
	}
	return d
}
