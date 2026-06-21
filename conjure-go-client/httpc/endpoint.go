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

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/internal"
	"github.com/palantir/pkg/bytesbuffers"
)

// RequestOverrides is the per-request configuration shared by [Endpoint] and
// [Overrides]. Every method is copy-on-write: it returns a new value with the
// override applied, so deriving variants from a shared base is safe:
//
//	base := ep.WithAddedHeader("X-Tenant", "acme")
//	v1 := base.WithAddedHeader("Api-Version", "1")
//	v2 := base.WithAddedHeader("Api-Version", "2") // base and v1 are unaffected
//
// The type parameter D is the concrete implementing type, so methods on
// Endpoint return Endpoint and methods on Overrides return Overrides.
//
// The two implementations represent two configuration layers that are
// composed at execute time:
//
//   - On [Endpoint], these methods set static defaults baked into the
//     package-level descriptor (e.g. a constant Accept-Language header for
//     every call to a given RPC).
//   - On [Overrides], they capture caller-supplied per-request values that
//     a service-client struct merges in via [Endpoint.WithOverrides]
//     (e.g. headers derived from the call site context).
//
// Headers and query parameters accumulate across both layers; scalar values
// (timeout, error decoder, basic auth) are last-wins.
type RequestOverrides[D any] interface {
	// WithHeader sets a request header to the given value(s), replacing any
	// previously added or set values for the key.
	WithHeader(key, value string, additionalValues ...string) D
	// WithAddedHeader appends one or more values to a request header.
	// Multiple calls with the same key accumulate values.
	WithAddedHeader(key, value string, additionalValues ...string) D
	// WithQuery sets a query parameter to the given value(s), replacing any
	// previously added or set values for the key.
	WithQuery(key, value string, additionalValues ...string) D
	// WithAddedQuery appends one or more values to a query parameter.
	WithAddedQuery(key, value string, additionalValues ...string) D
	// WithAddedQueryValues appends every key/value pair in q to the request query.
	WithAddedQueryValues(q url.Values) D
	// WithTimeout sets a per-attempt timeout that overrides the client-level
	// timeout. [Send] applies it to each attempt via the call-scoped
	// *http.Client. Use a context deadline for a whole-call deadline that spans
	// all retries.
	WithTimeout(time.Duration) D
	// WithErrorDecoder sets a per-request error decoder; overrides the
	// endpoint-level decoder and [DefaultErrorDecoder]. For typed Conjure errors,
	// use conjureerrors.WithConjureErrorDecoder, which keeps the
	// conjure-go-contract/errors dependency off this interface.
	WithErrorDecoder(ErrorDecoder) D
	// WithBasicAuth sets per-request basic auth credentials, overriding any client-level auth.
	WithBasicAuth(user, password string) D
	// WithMiddleware appends a per-request middleware that runs once per attempt
	// around the resolved request, inside telemetry like the builder middleware.
	WithMiddleware(Middleware) D
	// WithBufferPool sets a buffer pool that encoders may use to avoid
	// per-request allocations. Pass nil to clear. The [bytesbuffers.Pool]
	// dependency is intentional — mocks of this interface need to import it.
	WithBufferPool(bytesbuffers.Pool) D
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
//	resp, _, err := createItem.WithAddedHeader("Idempotency-Key", key).Execute(ctx, client, req)
//
// Endpoint implements all of [RequestOverrides]; values set this way are
// static defaults attached to the package-level descriptor. Caller-supplied
// per-request configuration belongs on a separate [Overrides] value merged in
// via [Endpoint.WithOverrides]; the two layers compose at execute time
// (additive for headers/query, last-wins for scalars).
type Endpoint[Req, Resp any] struct {
	method    string
	path      string
	name      string
	accept    string
	overrides Overrides
	encoder   BodyEncoder[Req]
	decoder   BodyDecoder[Resp]
	body      Req
	hasBody   bool
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
		WithDecoder(JSONDecoder[Resp]()).
		WithAccept("application/json")
}

// NewJSONPOST is NewPOST preconfigured with JSON encoder, JSON decoder, and Accept: application/json.
func NewJSONPOST[Req, Resp any](name, path string) Endpoint[Req, Resp] {
	return NewPOST[Req, Resp](name, path).
		WithEncoder(JSONEncoder[Req]()).
		WithDecoder(JSONDecoder[Resp]()).
		WithAccept("application/json")
}

// NewJSONPUT is NewPUT preconfigured with JSON encoder, JSON decoder, and Accept: application/json.
func NewJSONPUT[Req, Resp any](name, path string) Endpoint[Req, Resp] {
	return NewPUT[Req, Resp](name, path).
		WithEncoder(JSONEncoder[Req]()).
		WithDecoder(JSONDecoder[Resp]()).
		WithAccept("application/json")
}

// NewJSONPATCH is NewPATCH preconfigured with JSON encoder, JSON decoder, and Accept: application/json.
func NewJSONPATCH[Req, Resp any](name, path string) Endpoint[Req, Resp] {
	return NewPATCH[Req, Resp](name, path).
		WithEncoder(JSONEncoder[Req]()).
		WithDecoder(JSONDecoder[Resp]()).
		WithAccept("application/json")
}

// NewJSONDELETE is NewDELETE preconfigured with a JSON decoder and Accept: application/json.
func NewJSONDELETE[Resp any](name, path string) Endpoint[Void, Resp] {
	return NewDELETE[Resp](name, path).
		WithDecoder(JSONDecoder[Resp]()).
		WithAccept("application/json")
}

// WithEncoder sets the body encoder for the request.
func (e Endpoint[Req, Resp]) WithEncoder(enc BodyEncoder[Req]) Endpoint[Req, Resp] {
	e.encoder = enc
	return e
}

// WithDecoder sets the body decoder for the response.
func (e Endpoint[Req, Resp]) WithDecoder(dec BodyDecoder[Resp]) Endpoint[Req, Resp] {
	e.decoder = dec
	return e
}

// WithAccept sets the Accept header. Pass "" to send no Accept header (the default).
// Per-request WithHeader("Accept", ...) overrides this.
func (e Endpoint[Req, Resp]) WithAccept(accept string) Endpoint[Req, Resp] {
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
	} else {
		e.path = strings.ReplaceAll(e.path, "{"+key+"}", url.PathEscape(s))
	}
	return e
}

// WithHeader sets a header to the given value(s), replacing any previously
// added or set values for the key.
func (e Endpoint[Req, Resp]) WithHeader(key, value string, additionalValues ...string) Endpoint[Req, Resp] {
	e.overrides = e.overrides.WithHeader(key, value, additionalValues...)
	return e
}

// WithAddedHeader appends one or more values to a header; multiple calls with
// the same key accumulate.
func (e Endpoint[Req, Resp]) WithAddedHeader(key, value string, additionalValues ...string) Endpoint[Req, Resp] {
	e.overrides = e.overrides.WithAddedHeader(key, value, additionalValues...)
	return e
}

// WithQuery sets a query parameter to the given value(s), replacing any
// previously added or set values for the key.
func (e Endpoint[Req, Resp]) WithQuery(key, value string, additionalValues ...string) Endpoint[Req, Resp] {
	e.overrides = e.overrides.WithQuery(key, value, additionalValues...)
	return e
}

// WithAddedQuery appends one or more values to a query parameter.
func (e Endpoint[Req, Resp]) WithAddedQuery(key, value string, additionalValues ...string) Endpoint[Req, Resp] {
	e.overrides = e.overrides.WithAddedQuery(key, value, additionalValues...)
	return e
}

// WithAddedQueryValues appends every key/value pair in q to the request query.
func (e Endpoint[Req, Resp]) WithAddedQueryValues(q url.Values) Endpoint[Req, Resp] {
	e.overrides = e.overrides.WithAddedQueryValues(q)
	return e
}

// WithTimeout sets a per-attempt timeout that overrides the client-level
// timeout. [Send] applies it to each attempt via the call-scoped *http.Client.
// Use a context deadline for a whole-call deadline that spans all retries.
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

// WithBufferPool sets a buffer pool that encoders may use to avoid per-request
// allocations. Conjure-generated code sets this from endpoint tags such as
// request-buffer-medium. Pass nil to clear.
func (e Endpoint[Req, Resp]) WithBufferPool(p bytesbuffers.Pool) Endpoint[Req, Resp] {
	e.overrides = e.overrides.WithBufferPool(p)
	return e
}

// WithBody attaches a request body to the endpoint. The body is encoded by
// the endpoint's [BodyEncoder] when [Endpoint.Execute] runs. Endpoints with
// Req = [Void] never need WithBody. For other Req, omitting WithBody sends no
// body (the encoder is not invoked).
func (e Endpoint[Req, Resp]) WithBody(body Req) Endpoint[Req, Resp] {
	e.body = body
	e.hasBody = true
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
// client.Do, and decodes the response. Use [Endpoint.WithBody] to attach a
// request body; endpoints without a body (including those with Req = [Void])
// can call Execute directly.
//
// The returned *http.Response has its body consumed (drained or handed to the
// decoder). It may be non-nil on error when the server replied but the decoded
// response represents a failure.
func (e Endpoint[Req, Resp]) Execute(ctx context.Context, client Client) (Resp, *http.Response, error) {
	var zero Resp

	if i := strings.IndexByte(e.path, '{'); i != -1 {
		j := strings.IndexByte(e.path[i:], '}')
		if j == -1 {
			return zero, nil, fmt.Errorf("httpc: unterminated path parameter starting at %q in %s %s", e.path[i:], e.method, e.path)
		}
		param := e.path[i : i+j+1]
		return zero, nil, fmt.Errorf("httpc: path parameter %s not populated in %s %s", param, e.method, e.path)
	}

	if e.name != "" {
		ctx = ContextWithRPCMethodName(ctx, e.name)
	}
	if e.overrides.bufferPool != nil {
		ctx = contextWithBufferPool(ctx, e.overrides.bufferPool)
	}

	// Path-only request; Client prepends the base URI on each attempt.
	req, err := http.NewRequestWithContext(ctx, e.method, e.path, nil)
	if err != nil {
		return zero, nil, err
	}

	if e.hasBody {
		if e.encoder == nil {
			return zero, nil, fmt.Errorf("httpc: endpoint %s has a body but no encoder; call WithEncoder before WithBody", e.name)
		}
		if err := e.encoder.Encode(req, e.body); err != nil {
			return zero, nil, err
		}
	}
	if e.accept != "" {
		req.Header.Set("Accept", e.accept)
	}

	// Headers and query travel as RequestValues so the runtime resolves them per
	// attempt above its intrinsic values — they take precedence over client auth
	// and headers without eagerly mutating req (and without invoking an overridden
	// auth provider). Per-request middlewares run innermost (per attempt, with the
	// resolved URL). A per-request timeout overrides the per-attempt bound; a
	// total-call deadline is the caller's job via ctx.
	opts := SendOptions{
		Values:      e.overrides.requestValues(),
		Middlewares: e.overrides.middlewares,
		Policy:      e.overrides.callPolicyOverrides(),
	}

	resp, err := client.Send(ctx, req, opts)
	if err != nil {
		return zero, nil, err
	}

	decoder := e.overrides.errorDecoder
	if decoder == nil {
		decoder = DefaultErrorDecoder()
	}
	if decoder.Handles(resp) {
		decodeErr := decoder.DecodeError(resp)
		internal.DrainBody(ctx, resp)
		return zero, resp, decodeErr
	}

	if e.decoder != nil {
		result, err := e.decoder.Decode(ctx, resp)
		if err != nil {
			internal.DrainBody(ctx, resp)
			return zero, resp, err
		}
		// rawBodyDecoder hands the body to the caller; don't drain.
		if _, raw := e.decoder.(rawBodyDecoder); !raw {
			internal.DrainBody(ctx, resp)
		}
		return result, resp, nil
	}
	internal.DrainBody(ctx, resp)
	return zero, resp, nil
}

// WithTraceHeader sets the X-B3-TraceId header on any RequestOverrides value.
func WithTraceHeader[D RequestOverrides[D]](d D, traceID string) D {
	return d.WithHeader("X-B3-TraceId", traceID)
}
