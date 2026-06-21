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
// described by an endpoint ([NoBodyEndpoint] or [BodyEndpoint]) that pairs an
// HTTP method and path template with a typed encoder and decoder.
//
// # Quick start
//
//	client, err := httpc.NewBuilder().
//	    SetServiceName("item-service").
//	    SetBaseURLs("https://item-service.example.com").
//	    SetAuthToken(token).
//	    Build(ctx)
//
//	var getItem = httpc.NewGET[GetItemResponse]("GetItem", "/api/v1/items/{itemId}").WithJSON()
//
//	resp, _, err := getItem.Call().
//	    WithPathParam("itemId", "item-42").
//	    Execute(ctx, client)
//
// # Core concepts
//
//   - [Runtime] sends a request to a configured service: it owns base-URL
//     selection, retries, and the default call policy. [Call.Execute] drives
//     requests through it; [Builder.Build] returns the standard implementation.
//   - An endpoint ([NoBodyEndpoint] or [BodyEndpoint]) is a copy-on-write
//     per-RPC descriptor. Store one as a package-level var; derive per-call
//     variants via its With* methods, then start an invocation with Call —
//     which yields a [Call] carrying the per-request methods (WithPathParam,
//     WithOverrides, Execute).
//   - [Overrides] is per-request configuration that merges into a [Call]
//     via WithOverrides. Generated service clients typically embed one.
//   - [Builder] is mutable. Use [Builder.Clone] before sharing across
//     goroutines. Most settings have refreshable counterparts (SetFooRefreshable)
//     for dynamic config.
//
// # Retry and error handling
//
// [Runtime.Send] — which [Call.Execute] invokes — retries on transport errors, 429,
// 503, and 307/308 (Conjure QoS redirect); other 4xx/5xx responses are not
// retried. Tune with
// [Builder.SetMaxAttempts] (nil = default, 0 = unlimited, n > 0 = exactly n),
// [Builder.SetInitialBackoff], and [Builder.SetMaxBackoff].
//
// Responses with status >= 307 are converted to errors by [DefaultErrorDecoder].
// Extract the status with [StatusCodeFromError]. Override the decoder per
// endpoint or per request via WithErrorDecoder; pass [NoErrorDecoder] to
// disable error decoding for that endpoint/call.
//
// # Concurrency
//
// Endpoints ([NoBodyEndpoint], [BodyEndpoint]), [Overrides], and clients
// returned by [Builder.Build] are safe for concurrent use. [Builder] is not —
// call Clone before sharing.
//
// See README.md for the generated-service-client pattern and the metrics
// catalog; MIGRATION.md covers migrating from the sibling httpclient package.
package httpc
