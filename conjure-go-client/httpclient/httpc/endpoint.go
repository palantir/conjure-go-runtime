package httpc

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient/internal"
)

// rpcMethodNameKey is the context key for the RPC method name set by Endpoint.Execute.
type rpcMethodNameKey struct{}

// RPCMethodName extracts the RPC method name from the context, if set by Endpoint.Execute.
func RPCMethodName(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(rpcMethodNameKey{}).(string)
	return v, ok
}

// ContextWithRPCMethodName returns a new context with the RPC method name set for use in logging and metrics.
func ContextWithRPCMethodName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, rpcMethodNameKey{}, name)
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
//	base := ep.WithHeader("X-Tenant", "acme")
//	v1 := base.WithHeader("Api-Version", "1")
//	v2 := base.WithHeader("Api-Version", "2") // base and v1 are unaffected
//
// The type parameter D is the concrete implementing type (F-bounded polymorphism),
// ensuring that methods on Endpoint return Endpoint and methods on Overrides
// return Overrides.
type RequestOverrides[D any] interface {
	// WithHeader adds a request header. Multiple calls with the same key accumulate values.
	WithHeader(key, value string) D
	// WithQueryParam adds a query parameter. Multiple calls with the same key accumulate values.
	WithQueryParam(key, value string) D
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
// and the RequestOverrides With* methods) return a new Endpoint value without modifying
// the original. This makes Endpoint safe to store as a package-level base configuration
// and derive per-call variants from it concurrently:
//
//	// Package-level base endpoint.
//	var createItem = httpc.NewEndpoint[CreateReq, CreateResp](http.MethodPost, "/api/v1/items", "CreateItem").
//	    SetEncoder(httpc.JSONEncoder[CreateReq]()).
//	    SetDecoder(httpc.JSONDecoder[CreateResp]()).
//	    SetAccept("application/json")
//
//	// Per-call customization (does not modify createItem).
//	resp, err := createItem.WithHeader("Idempotency-Key", key).Execute(ctx, client, req)
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
// overridden per-request via WithHeader("Accept", ...).
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

// WithHeader adds a header to the request. Returns a new Endpoint value.
func (e Endpoint[Req, Resp]) WithHeader(key, value string) Endpoint[Req, Resp] {
	e.overrides = e.overrides.WithHeader(key, value)
	return e
}

// WithQueryParam adds a query parameter to the request. Returns a new Endpoint value.
func (e Endpoint[Req, Resp]) WithQueryParam(key, value string) Endpoint[Req, Resp] {
	e.overrides = e.overrides.WithQueryParam(key, value)
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

// WithOverrides merges the given Overrides into the endpoint. Headers and query params
// are additive; timeout, error decoder, and basic auth use last-wins; middlewares are appended.
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
func (e Endpoint[Req, Resp]) Execute(ctx context.Context, client Client, body Req) (Resp, error) {
	var zero Resp

	// Verify all path template parameters have been filled in.
	if i := strings.IndexByte(e.path, '{'); i != -1 {
		if j := strings.IndexByte(e.path[i:], '}'); j != -1 {
			param := e.path[i : i+j+1]
			return zero, fmt.Errorf("httpc: path parameter %s not populated in %s %s", param, e.method, e.path)
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
		return zero, err
	}

	// Encode body (sets Content-Type, Body, GetBody, ContentLength).
	if e.encoder != nil {
		if err := e.encoder.Encode(req, body); err != nil {
			return zero, err
		}
	}

	// Set Accept header.
	if e.accept != "" {
		req.Header.Set("Accept", e.accept)
	}

	// Apply per-endpoint headers (additive, after Accept/Content-Type).
	for k, vs := range e.overrides.headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	// Apply query params.
	if len(e.overrides.queryParams) > 0 {
		req.URL.RawQuery = e.overrides.queryParams.Encode()
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
		return zero, err
	}

	// Per-endpoint error decoding (before response decode).
	if e.overrides.errorDecoder != nil && e.overrides.errorDecoder.Handles(resp) {
		internal.DrainBody(ctx, resp)
		return zero, e.overrides.errorDecoder.DecodeError(resp)
	}

	// Decode response.
	if e.decoder != nil {
		result, err := e.decoder.Decode(ctx, resp)
		if err != nil {
			internal.DrainBody(ctx, resp)
			return zero, err
		}
		// Skip draining for decoders that return the body directly to the caller.
		if _, raw := e.decoder.(rawBodyDecoder); !raw {
			internal.DrainBody(ctx, resp)
		}
		return result, nil
	}
	// No decoder: drain body.
	internal.DrainBody(ctx, resp)
	return zero, nil
}

// ExecuteVoid is a convenience wrapper for executing endpoints with no request body
// (Req = struct{}), avoiding the need to pass an explicit struct{}{} at every call site.
func ExecuteVoid[Resp any](ctx context.Context, client Client, ep Endpoint[struct{}, Resp]) (Resp, error) {
	return ep.Execute(ctx, client, struct{}{})
}

// WithTraceHeader sets the X-B3-TraceId header on any RequestOverrides value.
// It works with both Endpoint and Overrides implementations.
func WithTraceHeader[D RequestOverrides[D]](d D, traceID string) D {
	return d.WithHeader("X-B3-TraceId", traceID)
}

// WithStandardHeaders sets all key-value pairs from headers on any RequestOverrides value.
// Iteration order over the map is non-deterministic, but since each WithHeader call
// adds (rather than replaces) headers, the final set of headers is always the same.
func WithStandardHeaders[D RequestOverrides[D]](d D, headers map[string]string) D {
	for k, v := range headers {
		d = d.WithHeader(k, v)
	}
	return d
}
