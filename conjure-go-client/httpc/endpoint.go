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
	"net/http"
	"net/url"
	"time"

	"github.com/palantir/pkg/bytesbuffers"
)

// Void is the Req or Resp type for endpoints with no request or response body.
type Void = struct{}

// endpointCore is the immutable descriptor state shared by [BodyEndpoint] and
// [NoBodyEndpoint]: the HTTP method, RPC name, path template, Accept header, the
// response decoder, and the endpoint-level static request defaults.
type endpointCore[Resp any] struct {
	method       string
	pathTemplate string
	name         string
	accept       string
	decoder      BodyDecoder[Resp]
	defaults     Overrides
}

// BodyEndpoint is a copy-on-write descriptor for an RPC that sends a request
// body: an HTTP method and path template paired with a typed encoder/decoder.
// Methods return a new value, so a BodyEndpoint is safe to store as a
// package-level var and derive variants from. Construct via [NewPOST], [NewPUT],
// [NewPATCH], or [NewBodyEndpoint] for an arbitrary method.
//
//	var createItem = httpc.NewPOST[CreateReq, CreateResp]("CreateItem", "/api/v1/items").WithJSON()
//	resp, _, err := createItem.Call(req).WithAddedHeader("Idempotency-Key", key).Execute(ctx, client)
//
// A BodyEndpoint produces a per-invocation [Call] via [BodyEndpoint.Call], which
// takes the request body — so a body endpoint cannot be executed without one.
// Configuration set directly on the descriptor is a static default; per-call
// configuration belongs on the [Call] (see [RequestOverrides]).
type BodyEndpoint[Req, Resp any] struct {
	core    endpointCore[Resp]
	encoder BodyEncoder[Req]
}

// NoBodyEndpoint is a copy-on-write descriptor for an RPC that sends no request
// body (GET/DELETE/HEAD). Like [BodyEndpoint] it is safe to store as a
// package-level var. Construct via [NewGET], [NewDELETE], [NewHEAD], or
// [NewNoBodyEndpoint] for an arbitrary bodyless method.
//
//	var getItem = httpc.NewGET[GetItemResponse]("GetItem", "/api/v1/items/{itemId}").WithJSON()
//	resp, _, err := getItem.Call().WithPathParam("itemId", id).Execute(ctx, client)
//
// A NoBodyEndpoint produces a per-invocation [Call] via [NoBodyEndpoint.Call],
// which takes no body.
type NoBodyEndpoint[Resp any] struct {
	core endpointCore[Resp]
}

// NewBodyEndpoint creates a [BodyEndpoint] with the given method, RPC name, and
// path template. The RPC name is used in tracing spans and metrics tags.
func NewBodyEndpoint[Req, Resp any](method, name, path string) BodyEndpoint[Req, Resp] {
	return BodyEndpoint[Req, Resp]{core: endpointCore[Resp]{method: method, pathTemplate: path, name: name}}
}

// NewNoBodyEndpoint creates a [NoBodyEndpoint] with the given (bodyless) method,
// RPC name, and path template.
func NewNoBodyEndpoint[Resp any](method, name, path string) NoBodyEndpoint[Resp] {
	return NoBodyEndpoint[Resp]{core: endpointCore[Resp]{method: method, pathTemplate: path, name: name}}
}

// NewGET creates a GET endpoint with no request body.
func NewGET[Resp any](name, path string) NoBodyEndpoint[Resp] {
	return NewNoBodyEndpoint[Resp](http.MethodGet, name, path)
}

// NewDELETE creates a DELETE endpoint with no request body.
func NewDELETE[Resp any](name, path string) NoBodyEndpoint[Resp] {
	return NewNoBodyEndpoint[Resp](http.MethodDelete, name, path)
}

// NewHEAD creates a HEAD endpoint with no request body.
func NewHEAD[Resp any](name, path string) NoBodyEndpoint[Resp] {
	return NewNoBodyEndpoint[Resp](http.MethodHead, name, path)
}

// NewPOST creates a POST endpoint with a typed request body.
func NewPOST[Req, Resp any](name, path string) BodyEndpoint[Req, Resp] {
	return NewBodyEndpoint[Req, Resp](http.MethodPost, name, path)
}

// NewPUT creates a PUT endpoint with a typed request body.
func NewPUT[Req, Resp any](name, path string) BodyEndpoint[Req, Resp] {
	return NewBodyEndpoint[Req, Resp](http.MethodPut, name, path)
}

// NewPATCH creates a PATCH endpoint with a typed request body.
func NewPATCH[Req, Resp any](name, path string) BodyEndpoint[Req, Resp] {
	return NewBodyEndpoint[Req, Resp](http.MethodPatch, name, path)
}

// --- BodyEndpoint descriptor configuration ---

// WithEncoder sets the body encoder for the request.
func (e BodyEndpoint[Req, Resp]) WithEncoder(enc BodyEncoder[Req]) BodyEndpoint[Req, Resp] {
	e.encoder = enc
	return e
}

// WithDecoder sets the body decoder for the response.
func (e BodyEndpoint[Req, Resp]) WithDecoder(dec BodyDecoder[Resp]) BodyEndpoint[Req, Resp] {
	e.core.decoder = dec
	return e
}

// WithAccept sets the Accept header. Pass "" to send no Accept header (the default).
// A per-call WithHeader("Accept", ...) overrides this.
func (e BodyEndpoint[Req, Resp]) WithAccept(accept string) BodyEndpoint[Req, Resp] {
	e.core.accept = accept
	return e
}

// WithJSON configures JSON for both directions: a JSON request encoder, a JSON
// response decoder, and Accept: application/json. It is sugar for
// WithEncoder(JSONEncoder[Req]()).WithDecoder(JSONDecoder[Resp]()).WithAccept("application/json").
func (e BodyEndpoint[Req, Resp]) WithJSON() BodyEndpoint[Req, Resp] {
	e.encoder = JSONEncoder[Req]()
	e.core.decoder = JSONDecoder[Resp]()
	e.core.accept = "application/json"
	return e
}

func (e BodyEndpoint[Req, Resp]) WithHeader(key, value string, additionalValues ...string) BodyEndpoint[Req, Resp] {
	e.core.defaults = e.core.defaults.WithHeader(key, value, additionalValues...)
	return e
}

func (e BodyEndpoint[Req, Resp]) WithAddedHeader(key, value string, additionalValues ...string) BodyEndpoint[Req, Resp] {
	e.core.defaults = e.core.defaults.WithAddedHeader(key, value, additionalValues...)
	return e
}

func (e BodyEndpoint[Req, Resp]) WithQuery(key, value string, additionalValues ...string) BodyEndpoint[Req, Resp] {
	e.core.defaults = e.core.defaults.WithQuery(key, value, additionalValues...)
	return e
}

func (e BodyEndpoint[Req, Resp]) WithAddedQuery(key, value string, additionalValues ...string) BodyEndpoint[Req, Resp] {
	e.core.defaults = e.core.defaults.WithAddedQuery(key, value, additionalValues...)
	return e
}

func (e BodyEndpoint[Req, Resp]) WithAddedQueryValues(q url.Values) BodyEndpoint[Req, Resp] {
	e.core.defaults = e.core.defaults.WithAddedQueryValues(q)
	return e
}

func (e BodyEndpoint[Req, Resp]) WithTimeout(d time.Duration) BodyEndpoint[Req, Resp] {
	e.core.defaults = e.core.defaults.WithTimeout(d)
	return e
}

func (e BodyEndpoint[Req, Resp]) WithUnlimitedTimeout() BodyEndpoint[Req, Resp] {
	e.core.defaults = e.core.defaults.WithUnlimitedTimeout()
	return e
}

func (e BodyEndpoint[Req, Resp]) WithDefaultTimeout() BodyEndpoint[Req, Resp] {
	e.core.defaults = e.core.defaults.WithDefaultTimeout()
	return e
}

func (e BodyEndpoint[Req, Resp]) WithErrorDecoder(d ErrorDecoder) BodyEndpoint[Req, Resp] {
	e.core.defaults = e.core.defaults.WithErrorDecoder(d)
	return e
}

func (e BodyEndpoint[Req, Resp]) WithNoErrorDecoder() BodyEndpoint[Req, Resp] {
	e.core.defaults = e.core.defaults.WithNoErrorDecoder()
	return e
}

func (e BodyEndpoint[Req, Resp]) WithDefaultErrorDecoder() BodyEndpoint[Req, Resp] {
	e.core.defaults = e.core.defaults.WithDefaultErrorDecoder()
	return e
}

func (e BodyEndpoint[Req, Resp]) WithAuthorization(a Authorizer) BodyEndpoint[Req, Resp] {
	e.core.defaults = e.core.defaults.WithAuthorization(a)
	return e
}

func (e BodyEndpoint[Req, Resp]) WithDefaultAuthorization() BodyEndpoint[Req, Resp] {
	e.core.defaults = e.core.defaults.WithDefaultAuthorization()
	return e
}

func (e BodyEndpoint[Req, Resp]) WithMiddleware(m Middleware) BodyEndpoint[Req, Resp] {
	e.core.defaults = e.core.defaults.WithMiddleware(m)
	return e
}

func (e BodyEndpoint[Req, Resp]) WithBufferPool(p bytesbuffers.Pool) BodyEndpoint[Req, Resp] {
	e.core.defaults = e.core.defaults.WithBufferPool(p)
	return e
}

func (e BodyEndpoint[Req, Resp]) WithDefaultBufferPool() BodyEndpoint[Req, Resp] {
	e.core.defaults = e.core.defaults.WithDefaultBufferPool()
	return e
}

// Call begins a request invocation with the given body, which the endpoint's
// encoder encodes when [Call.Execute] runs. Per-call configuration and Execute
// live on the returned [Call].
func (e BodyEndpoint[Req, Resp]) Call(body Req) Call[Resp] {
	encoder := e.encoder
	name := e.core.name
	return newCall(e.core, func(req *http.Request) error {
		if encoder == nil {
			return errNoEncoder(name)
		}
		return encoder.Encode(req, body)
	})
}

// --- NoBodyEndpoint descriptor configuration ---

// WithDecoder sets the body decoder for the response.
func (e NoBodyEndpoint[Resp]) WithDecoder(dec BodyDecoder[Resp]) NoBodyEndpoint[Resp] {
	e.core.decoder = dec
	return e
}

// WithAccept sets the Accept header. Pass "" to send no Accept header (the default).
// A per-call WithHeader("Accept", ...) overrides this.
func (e NoBodyEndpoint[Resp]) WithAccept(accept string) NoBodyEndpoint[Resp] {
	e.core.accept = accept
	return e
}

// WithJSON configures a JSON response decoder and Accept: application/json. It is
// sugar for WithDecoder(JSONDecoder[Resp]()).WithAccept("application/json"). There
// is no request encoder — a NoBodyEndpoint sends no body.
func (e NoBodyEndpoint[Resp]) WithJSON() NoBodyEndpoint[Resp] {
	e.core.decoder = JSONDecoder[Resp]()
	e.core.accept = "application/json"
	return e
}

func (e NoBodyEndpoint[Resp]) WithHeader(key, value string, additionalValues ...string) NoBodyEndpoint[Resp] {
	e.core.defaults = e.core.defaults.WithHeader(key, value, additionalValues...)
	return e
}

func (e NoBodyEndpoint[Resp]) WithAddedHeader(key, value string, additionalValues ...string) NoBodyEndpoint[Resp] {
	e.core.defaults = e.core.defaults.WithAddedHeader(key, value, additionalValues...)
	return e
}

func (e NoBodyEndpoint[Resp]) WithQuery(key, value string, additionalValues ...string) NoBodyEndpoint[Resp] {
	e.core.defaults = e.core.defaults.WithQuery(key, value, additionalValues...)
	return e
}

func (e NoBodyEndpoint[Resp]) WithAddedQuery(key, value string, additionalValues ...string) NoBodyEndpoint[Resp] {
	e.core.defaults = e.core.defaults.WithAddedQuery(key, value, additionalValues...)
	return e
}

func (e NoBodyEndpoint[Resp]) WithAddedQueryValues(q url.Values) NoBodyEndpoint[Resp] {
	e.core.defaults = e.core.defaults.WithAddedQueryValues(q)
	return e
}

func (e NoBodyEndpoint[Resp]) WithTimeout(d time.Duration) NoBodyEndpoint[Resp] {
	e.core.defaults = e.core.defaults.WithTimeout(d)
	return e
}

func (e NoBodyEndpoint[Resp]) WithUnlimitedTimeout() NoBodyEndpoint[Resp] {
	e.core.defaults = e.core.defaults.WithUnlimitedTimeout()
	return e
}

func (e NoBodyEndpoint[Resp]) WithDefaultTimeout() NoBodyEndpoint[Resp] {
	e.core.defaults = e.core.defaults.WithDefaultTimeout()
	return e
}

func (e NoBodyEndpoint[Resp]) WithErrorDecoder(d ErrorDecoder) NoBodyEndpoint[Resp] {
	e.core.defaults = e.core.defaults.WithErrorDecoder(d)
	return e
}

func (e NoBodyEndpoint[Resp]) WithNoErrorDecoder() NoBodyEndpoint[Resp] {
	e.core.defaults = e.core.defaults.WithNoErrorDecoder()
	return e
}

func (e NoBodyEndpoint[Resp]) WithDefaultErrorDecoder() NoBodyEndpoint[Resp] {
	e.core.defaults = e.core.defaults.WithDefaultErrorDecoder()
	return e
}

func (e NoBodyEndpoint[Resp]) WithAuthorization(a Authorizer) NoBodyEndpoint[Resp] {
	e.core.defaults = e.core.defaults.WithAuthorization(a)
	return e
}

func (e NoBodyEndpoint[Resp]) WithDefaultAuthorization() NoBodyEndpoint[Resp] {
	e.core.defaults = e.core.defaults.WithDefaultAuthorization()
	return e
}

func (e NoBodyEndpoint[Resp]) WithMiddleware(m Middleware) NoBodyEndpoint[Resp] {
	e.core.defaults = e.core.defaults.WithMiddleware(m)
	return e
}

func (e NoBodyEndpoint[Resp]) WithBufferPool(p bytesbuffers.Pool) NoBodyEndpoint[Resp] {
	e.core.defaults = e.core.defaults.WithBufferPool(p)
	return e
}

func (e NoBodyEndpoint[Resp]) WithDefaultBufferPool() NoBodyEndpoint[Resp] {
	e.core.defaults = e.core.defaults.WithDefaultBufferPool()
	return e
}

// Call begins a request invocation with no body. Per-call configuration and
// Execute live on the returned [Call].
func (e NoBodyEndpoint[Resp]) Call() Call[Resp] {
	return newCall(e.core, nil)
}

// WithTraceHeader sets the X-B3-TraceId header on any [RequestOverrides] value —
// a descriptor (as a static default) or, more usefully, a [Call] or [Overrides]
// (per invocation).
func WithTraceHeader[D RequestOverrides[D]](d D, traceID string) D {
	return d.WithHeader("X-B3-TraceId", traceID)
}
