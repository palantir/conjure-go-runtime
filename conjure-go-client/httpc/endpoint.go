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

// RequestOverrides is the per-request configuration shared by [Endpoint] and
// [Overrides]. Every method is copy-on-write: it returns a new value with the
// override applied, so deriving variants from a shared base is safe:
//
//	base := ep.AddHeader("X-Tenant", "acme")
//	v1 := base.AddHeader("Api-Version", "1")
//	v2 := base.AddHeader("Api-Version", "2") // base and v1 are unaffected
//
// The type parameter D is the concrete implementing type, so methods on
// Endpoint return Endpoint and methods on Overrides return Overrides.
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

// Endpoint is a copy-on-write descriptor pairing an HTTP method and path with
// a typed encoder and decoder. Methods return a new value, so an Endpoint is
// safe to store as a package-level var and derive per-call variants from.
//
// Construct via [NewGET], [NewPOST], [NewJSONPOST], etc., or [NewEndpoint] for
// arbitrary methods. For body-less endpoints, Req is [Void] and Execute is
// called with httpc.Void{}.
//
//	var createItem = httpc.NewJSONPOST[CreateReq, CreateResp]("CreateItem", "/api/v1/items")
//	resp, _, err := createItem.AddHeader("Idempotency-Key", key).Execute(ctx, client, req)
//
// Endpoint also implements all of [RequestOverrides], plus [Endpoint.WithOverrides]
// for merging a separately built [Overrides] value.
type Endpoint[Req, Resp any] struct {
	method    string
	path      string
	name      string
	accept    string
	overrides Overrides
	encoder   BodyEncoder[Req]
	decoder   BodyDecoder[Resp]
}

// NewEndpoint creates an Endpoint with the given method, RPC name, and path
// template. The RPC name is used in tracing spans and metrics tags.
func NewEndpoint[Req, Resp any](method, name, path string) Endpoint[Req, Resp] {
	return Endpoint[Req, Resp]{
		method: method,
		path:   path,
		name:   name,
	}
}

// NewGET creates a GET endpoint with no request body.
func NewGET[Resp any](name, path string) Endpoint[Void, Resp] {
	return NewEndpoint[Void, Resp](http.MethodGet, name, path)
}

// NewDELETE creates a DELETE endpoint with no request body.
func NewDELETE[Resp any](name, path string) Endpoint[Void, Resp] {
	return NewEndpoint[Void, Resp](http.MethodDelete, name, path)
}

// NewHEAD creates a HEAD endpoint with no request body.
func NewHEAD[Resp any](name, path string) Endpoint[Void, Resp] {
	return NewEndpoint[Void, Resp](http.MethodHead, name, path)
}

// NewPOST creates a POST endpoint with a typed request body.
func NewPOST[Req, Resp any](name, path string) Endpoint[Req, Resp] {
	return NewEndpoint[Req, Resp](http.MethodPost, name, path)
}

// NewPUT creates a PUT endpoint with a typed request body.
func NewPUT[Req, Resp any](name, path string) Endpoint[Req, Resp] {
	return NewEndpoint[Req, Resp](http.MethodPut, name, path)
}

// NewPATCH creates a PATCH endpoint with a typed request body.
func NewPATCH[Req, Resp any](name, path string) Endpoint[Req, Resp] {
	return NewEndpoint[Req, Resp](http.MethodPatch, name, path)
}

// NewJSONGET is NewGET preconfigured with a JSON decoder and Accept: application/json.
func NewJSONGET[Resp any](name, path string) Endpoint[Void, Resp] {
	return NewGET[Resp](name, path).
		SetDecoder(JSONDecoder[Resp]()).
		SetAccept("application/json")
}

// NewJSONPOST is NewPOST preconfigured with JSON encoder, JSON decoder, and Accept: application/json.
func NewJSONPOST[Req, Resp any](name, path string) Endpoint[Req, Resp] {
	return NewPOST[Req, Resp](name, path).
		SetEncoder(JSONEncoder[Req]()).
		SetDecoder(JSONDecoder[Resp]()).
		SetAccept("application/json")
}

// NewJSONPUT is NewPUT preconfigured with JSON encoder, JSON decoder, and Accept: application/json.
func NewJSONPUT[Req, Resp any](name, path string) Endpoint[Req, Resp] {
	return NewPUT[Req, Resp](name, path).
		SetEncoder(JSONEncoder[Req]()).
		SetDecoder(JSONDecoder[Resp]()).
		SetAccept("application/json")
}

// NewJSONPATCH is NewPATCH preconfigured with JSON encoder, JSON decoder, and Accept: application/json.
func NewJSONPATCH[Req, Resp any](name, path string) Endpoint[Req, Resp] {
	return NewPATCH[Req, Resp](name, path).
		SetEncoder(JSONEncoder[Req]()).
		SetDecoder(JSONDecoder[Resp]()).
		SetAccept("application/json")
}

// NewJSONDELETE is NewDELETE preconfigured with a JSON decoder and Accept: application/json.
func NewJSONDELETE[Resp any](name, path string) Endpoint[Void, Resp] {
	return NewDELETE[Resp](name, path).
		SetDecoder(JSONDecoder[Resp]()).
		SetAccept("application/json")
}

// SetEncoder sets the body encoder for the request.
func (e Endpoint[Req, Resp]) SetEncoder(enc BodyEncoder[Req]) Endpoint[Req, Resp] {
	e.encoder = enc
	return e
}

// SetDecoder sets the body decoder for the response.
func (e Endpoint[Req, Resp]) SetDecoder(dec BodyDecoder[Resp]) Endpoint[Req, Resp] {
	e.decoder = dec
	return e
}

// SetAccept sets the Accept header. Pass "" to send no Accept header (the default).
// Per-request SetHeader("Accept", ...) overrides this.
func (e Endpoint[Req, Resp]) SetAccept(accept string) Endpoint[Req, Resp] {
	e.accept = accept
	return e
}

// WithPathParam fills a named {key} placeholder in the path template with
// url.PathEscape(fmt.Sprint(value)). Parameters can be filled in any order:
//
//	var ep = httpc.NewGET[Resp]("GetItem", "/items/{itemId}/version/{version}")
//	ep.WithPathParam("itemId", id).WithPathParam("version", v)
//
// A trailing greedy placeholder ({key*}) preserves slashes while still escaping
// each individual segment:
//
//	var ep = httpc.NewGET[Resp]("GetFile", "/files/{filePath*}")
//	ep.WithPathParam("filePath", "dir/sub dir/file.txt")
//	// → /files/dir/sub%20dir/file.txt
func (e Endpoint[Req, Resp]) WithPathParam(key string, value any) Endpoint[Req, Resp] {
	s := fmt.Sprint(value)
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

// AddHeader adds a header value; multiple calls with the same key accumulate.
func (e Endpoint[Req, Resp]) AddHeader(key, value string) Endpoint[Req, Resp] {
	e.overrides = e.overrides.AddHeader(key, value)
	return e
}

// SetHeader sets a header value, replacing any previously added or set values for the key.
func (e Endpoint[Req, Resp]) SetHeader(key, value string) Endpoint[Req, Resp] {
	e.overrides = e.overrides.SetHeader(key, value)
	return e
}

// AddQuery adds a query parameter; multiple calls with the same key accumulate.
func (e Endpoint[Req, Resp]) AddQuery(key, value string) Endpoint[Req, Resp] {
	e.overrides = e.overrides.AddQuery(key, value)
	return e
}

// SetQuery sets a query parameter, replacing any previously added or set values for the key.
func (e Endpoint[Req, Resp]) SetQuery(key, value string) Endpoint[Req, Resp] {
	e.overrides = e.overrides.SetQuery(key, value)
	return e
}

// WithTimeout sets a per-request timeout that overrides the client-level timeout.
func (e Endpoint[Req, Resp]) WithTimeout(d time.Duration) Endpoint[Req, Resp] {
	e.overrides = e.overrides.WithTimeout(d)
	return e
}

// WithErrorDecoder sets a per-request error decoder that overrides the client-level decoder.
func (e Endpoint[Req, Resp]) WithErrorDecoder(d ErrorDecoder) Endpoint[Req, Resp] {
	e.overrides = e.overrides.WithErrorDecoder(d)
	return e
}

// WithBasicAuth sets per-request basic auth credentials, overriding any client-level auth.
func (e Endpoint[Req, Resp]) WithBasicAuth(user, password string) Endpoint[Req, Resp] {
	e.overrides = e.overrides.WithBasicAuth(user, password)
	return e
}

// WithMiddleware appends a per-request middleware.
func (e Endpoint[Req, Resp]) WithMiddleware(m Middleware) Endpoint[Req, Resp] {
	e.overrides = e.overrides.WithMiddleware(m)
	return e
}

// WithOverrides merges o into the endpoint's per-request configuration: set
// headers/query replace and clear matching adds; add headers/query accumulate;
// timeout, error decoder, and basic auth are last-wins; middlewares append.
func (e Endpoint[Req, Resp]) WithOverrides(o Overrides) Endpoint[Req, Resp] {
	e.overrides = e.overrides.merge(o)
	return e
}

// Execute builds an *http.Request from the endpoint configuration, sends it via
// client.Do, and decodes the response. Per-request overrides on the endpoint
// apply on top of the client's defaults.
//
// The returned *http.Response has its body consumed (drained or handed to the
// decoder). It may be non-nil on error when the server replied but the decoded
// response represents a failure.
func (e Endpoint[Req, Resp]) Execute(ctx context.Context, client Client, body Req) (Resp, *http.Response, error) {
	var zero Resp

	if i := strings.IndexByte(e.path, '{'); i != -1 {
		if j := strings.IndexByte(e.path[i:], '}'); j != -1 {
			param := e.path[i : i+j+1]
			return zero, nil, fmt.Errorf("httpc: path parameter %s not populated in %s %s", param, e.method, e.path)
		}
	}

	if e.name != "" {
		ctx = ContextWithRPCMethodName(ctx, e.name)
	}
	ctx = contextWithClientBufferPool(ctx, client)

	// Request is path-only; Client prepends the base URI on each attempt.
	req, err := http.NewRequestWithContext(ctx, e.method, e.path, nil)
	if err != nil {
		return zero, nil, err
	}

	if e.encoder != nil {
		if err := e.encoder.Encode(req, body); err != nil {
			return zero, nil, err
		}
	}
	if e.accept != "" {
		req.Header.Set("Accept", e.accept)
	}

	// Set headers replace (including Accept/Content-Type); add headers accumulate on top.
	for k, vs := range e.overrides.setHeaders {
		req.Header[k] = append([]string(nil), vs...)
	}
	for k, vs := range e.overrides.addHeaders {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

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

	if e.overrides.basicAuth != nil {
		req.SetBasicAuth(e.overrides.basicAuth.user, e.overrides.basicAuth.password)
	}

	// Per-request timeout uses two mechanisms: ContextWithRequestTimeout signals
	// doOnce to override clientCopy.Timeout (so the client-level timeout doesn't
	// cap us), and context.WithTimeout enforces the deadline for Clients that
	// bypass doOnce (e.g. a plain *http.Client). A zero timeout drops the
	// client-level Timeout without imposing a context deadline.
	if e.overrides.timeout != nil {
		timeout := *e.overrides.timeout
		ctx = ContextWithRequestTimeout(ctx, timeout)
		if timeout != 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		req = req.WithContext(ctx)
	}

	// Extract the client error decoder before wrapping with middleware (which
	// hides the concrete type).
	var clientErrorDecoder ErrorDecoder
	if edp, ok := client.(errorDecoderProvider); ok {
		clientErrorDecoder = edp.getErrorDecoder()
	}

	// Wrap with per-endpoint middleware (last added is outermost).
	c := client
	for _, mw := range e.overrides.middlewares {
		if mw != nil {
			c = wrapClientMiddleware(c, mw)
		}
	}

	resp, err := c.Do(req)
	if err != nil {
		return zero, nil, err
	}

	// Per-request decoder first; if it doesn't Handle the response, fall through
	// to the client-level decoder.
	for _, ed := range [...]ErrorDecoder{e.overrides.errorDecoder, clientErrorDecoder} {
		if ed != nil && ed.Handles(resp) {
			decodeErr := ed.DecodeError(resp)
			drainBody(ctx, resp)
			return zero, resp, decodeErr
		}
	}

	if e.decoder != nil {
		result, err := e.decoder.Decode(ctx, resp)
		if err != nil {
			drainBody(ctx, resp)
			return zero, resp, err
		}
		// rawBodyDecoder hands the body to the caller; don't drain.
		if _, raw := e.decoder.(rawBodyDecoder); !raw {
			drainBody(ctx, resp)
		}
		return result, resp, nil
	}
	drainBody(ctx, resp)
	return zero, resp, nil
}

// WithTraceHeader sets the X-B3-TraceId header on any RequestOverrides value.
func WithTraceHeader[D RequestOverrides[D]](d D, traceID string) D {
	return d.SetHeader("X-B3-TraceId", traceID)
}

// WithStandardHeaders SetHeaders every key/value pair from headers on d.
func WithStandardHeaders[D RequestOverrides[D]](d D, headers map[string]string) D {
	for k, v := range headers {
		d = d.SetHeader(k, v)
	}
	return d
}
