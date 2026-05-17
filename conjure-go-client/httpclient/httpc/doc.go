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

// Package httpc provides a type-safe, endpoint-centric HTTP client for Go
// services. Clients are constructed with a fluent [Builder]; each RPC is
// described by an [Endpoint] that pairs an HTTP method and path template with
// a typed encoder and decoder.
//
// # Quick start
//
//	client, err := httpc.NewBuilder().
//	    SetServiceName("item-service").
//	    SetBaseURLs("https://item-service.example.com").
//	    SetAuthToken(token).
//	    Build(ctx)
//
//	var getItem = httpc.NewJSONGET[GetItemResponse]("GetItem", "/api/v1/items/{itemId}")
//
//	resp, _, err := getItem.WithPathParam("itemId", "item-42").
//	    Execute(ctx, client, httpc.Void{})
//
// # Core concepts
//
//   - [Client] is the minimal transport interface: Do(*http.Request) (*http.Response, error).
//     Built clients add base-URL selection, retries, middleware, and timeout enforcement.
//   - [Endpoint] is a copy-on-write descriptor for a single RPC. Store one as a
//     package-level var per RPC; derive per-call variants via its With* methods.
//   - [Overrides] holds per-request configuration (headers, query, timeout, auth,
//     middleware, error decoder) and merges into an Endpoint via WithOverrides.
//     Generated service clients typically embed an Overrides for per-client tuning.
//   - [Builder] is a mutable fluent builder. Setters modify the receiver; use
//     [Builder.Clone] to fork an independent copy before mutating in another
//     goroutine. Most settings have both static (SetFoo) and refreshable
//     (SetFooRefreshable) variants for dynamic configuration.
//
// # Encoders and decoders
//
// [JSONEncoder] / [JSONDecoder] cover JSON request and response bodies.
// [BinaryEncoder] and [BinaryDecoder] handle streamed io.ReadCloser payloads.
// [GZIPEncoder], [ZLIBEncoder], and [SnappyEncoder] wrap any inner encoder with
// compression. Custom codecs implement [BodyEncoder] / [BodyDecoder] or use the
// [NewBodyEncoderFunc] / [NewBodyDecoderFunc] adapters.
//
// # Retry and error handling
//
// [Client.Do] retries on transport errors, 429, 503, and 307/308 (QoS redirect)
// per the Conjure QoS protocol; other 4xx/5xx responses are not retried.
// Configure retry behavior via [Builder.SetMaxAttempts] (a *int: nil = default,
// 0 = unlimited, n > 0 = exactly n attempts), [Builder.SetInitialBackoff], and
// [Builder.SetMaxBackoff].
//
// By default, responses with status >= 307 are converted to errors by
// [DefaultErrorDecoder]. [StatusCodeFromError] and [LocationFromError] extract
// the status code and redirect target. Disable with [Builder.DisableRestErrors],
// or override per-request via [Overrides.WithErrorDecoder].
//
// # Concurrency
//
// [Endpoint], [Overrides], and clients returned by [Builder.Build] are safe for
// concurrent use. [Builder] is NOT — call Clone before sharing.
//
// # More
//
// See README.md in this package directory for the full guide, including
// generated-service-client patterns and the metrics catalog. MIGRATION.md
// documents the migration from the parent httpclient package.
package httpc
