package httpc

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// RequestOverrides defines per-request configuration methods shared by Endpoint and
// ServiceClient. Unlike builder interfaces, all methods use copy-on-write semantics:
// they return a new value with the override applied, leaving the original unchanged.
// This makes it safe to derive multiple specialized configurations from a shared base:
//
//	base := ep.WithHeader("X-Tenant", "acme")
//	v1 := base.WithHeader("Api-Version", "1")
//	v2 := base.WithHeader("Api-Version", "2") // base and v1 are unaffected
//
// The type parameter D is the concrete implementing type (F-bounded polymorphism),
// ensuring that methods on Endpoint return Endpoint and methods on a generated
// service client return that client's own type.
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

// basicAuthCreds holds basic auth credentials for endpoint-level overrides.
type basicAuthCreds struct {
	user     string
	password string
}

// endpointConfig holds per-endpoint request overrides.
type endpointConfig struct {
	headers      http.Header
	queryParams  url.Values
	timeout      *time.Duration
	errorDecoder ErrorDecoder
	basicAuth    *basicAuthCreds
	middlewares  []Middleware
}

// clone returns a deep copy of the endpoint configuration.
func (c endpointConfig) clone() endpointConfig {
	out := c
	if c.headers != nil {
		out.headers = c.headers.Clone()
	}
	if c.queryParams != nil {
		cp := make(url.Values, len(c.queryParams))
		for k, v := range c.queryParams {
			cp[k] = append([]string(nil), v...)
		}
		out.queryParams = cp
	}
	if c.middlewares != nil {
		out.middlewares = make([]Middleware, len(c.middlewares))
		copy(out.middlewares, c.middlewares)
	}
	if c.timeout != nil {
		out.timeout = new(*c.timeout)
	}
	if c.basicAuth != nil {
		out.basicAuth = new(*c.basicAuth)
	}
	return out
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
	method  string
	path    string
	name    string
	accept  string
	config  endpointConfig
	encoder BodyEncoder[Req]
	decoder BodyDecoder[Resp]
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

// WithHeader adds a header to the request. Returns a new Endpoint value.
func (e Endpoint[Req, Resp]) WithHeader(key, value string) Endpoint[Req, Resp] {
	e.config = e.config.clone()
	if e.config.headers == nil {
		e.config.headers = make(http.Header)
	}
	e.config.headers.Add(key, value)
	return e
}

// WithQueryParam adds a query parameter to the request. Returns a new Endpoint value.
func (e Endpoint[Req, Resp]) WithQueryParam(key, value string) Endpoint[Req, Resp] {
	e.config = e.config.clone()
	if e.config.queryParams == nil {
		e.config.queryParams = make(url.Values)
	}
	e.config.queryParams.Add(key, value)
	return e
}

// WithTimeout sets a per-request timeout override. Returns a new Endpoint value.
func (e Endpoint[Req, Resp]) WithTimeout(d time.Duration) Endpoint[Req, Resp] {
	e.config = e.config.clone()
	e.config.timeout = &d
	return e
}

// WithErrorDecoder sets a per-request error decoder override. Returns a new Endpoint value.
func (e Endpoint[Req, Resp]) WithErrorDecoder(d ErrorDecoder) Endpoint[Req, Resp] {
	e.config = e.config.clone()
	e.config.errorDecoder = d
	return e
}

// WithBasicAuth sets per-request basic auth credentials. Returns a new Endpoint value.
func (e Endpoint[Req, Resp]) WithBasicAuth(user, password string) Endpoint[Req, Resp] {
	e.config = e.config.clone()
	e.config.basicAuth = &basicAuthCreds{user: user, password: password}
	return e
}

// WithMiddleware appends a per-request middleware. Returns a new Endpoint value.
func (e Endpoint[Req, Resp]) WithMiddleware(m Middleware) Endpoint[Req, Resp] {
	e.config = e.config.clone()
	e.config.middlewares = append(e.config.middlewares, m)
	return e
}

// Execute performs the HTTP request using the given client and request body.
// It builds an *http.Request from the endpoint's method, path, headers, query parameters,
// and encoded body, then delegates to client.Do. The response is decoded using the
// configured BodyDecoder. Per-request overrides (timeout, error decoder, basic auth,
// middleware) are applied on top of the client's defaults.
func (e Endpoint[Req, Resp]) Execute(ctx context.Context, client Client, body Req) (Resp, error) {
	panic("not implemented")
}

// ExecuteVoid is a convenience wrapper for executing endpoints with no request body
// (Req = struct{}), avoiding the need to pass an explicit struct{}{} at every call site.
func ExecuteVoid[Resp any](ctx context.Context, client Client, ep Endpoint[struct{}, Resp]) (Resp, error) {
	return ep.Execute(ctx, client, struct{}{})
}

// WithTraceHeader sets the X-B3-TraceId header on any RequestOverrides value.
// It works with both Endpoint and ServiceClient implementations.
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
