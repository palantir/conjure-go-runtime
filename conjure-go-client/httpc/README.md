# httpc

Package `httpc` provides a type-safe, endpoint-centric HTTP client for Go services.
Clients are configured via fluent builders and make requests through reusable endpoint
descriptors that pair typed encoders/decoders with HTTP method and path templates. An
endpoint descriptor is immutable and reusable; each invocation derives a per-call `Call`
from it (carrying the body, path params, and any per-call overrides) and executes that.

If you are migrating from the sibling `httpclient` package, see [MIGRATION.md](MIGRATION.md).

## Quick start

```go
// 1. Build a client.
client, err := httpc.NewBuilder().
    SetServiceName("item-service").
    SetBaseURLs("https://item-service.example.com").
    SetAuthToken(token).
    Build(ctx)

// 2. Define an endpoint descriptor (typically a package-level var).
var getItem = httpc.NewGET[GetItemResponse]("GetItem", "/api/v1/items/{itemId}").WithJSON()

// 3. Derive a Call and execute it.
resp, _, err := getItem.Call().
    WithPathParam("itemId", "item-42").
    Execute(ctx, client)
```

## Runnable examples

The [`examples`](examples) package contains runnable Go examples that are validated
by `go test` and render in godoc. Useful starting points:

- Basic calls: [`Example_basicGet`](examples/example_basic_get_test.go),
  [`Example_postJSON`](examples/example_post_json_test.go)
- Request shape: [`Example_pathAndQueryParams`](examples/example_path_and_query_params_test.go),
  [`Example_greedyPathParam`](examples/example_greedy_path_param_test.go),
  [`Example_headers`](examples/example_headers_test.go)
- Auth: [`Example_bearerToken`](examples/example_auth_bearer_test.go),
  [`Example_basicAuth`](examples/example_auth_basic_test.go),
  [`Example_authPrecedence`](examples/example_auth_precedence_test.go)
- Body codecs: [`Example_binaryStreaming`](examples/example_binary_streaming_test.go),
  [`Example_replayableStreamingBody`](examples/example_replayable_streaming_body_test.go),
  [`Example_customCodec`](examples/example_custom_codec_test.go)
- Runtime and builders: [`Example_sendLowLevel`](examples/example_send_low_level_test.go),
  [`Example_sendRequestValues`](examples/example_send_request_values_test.go),
  [`Example_customClient`](examples/example_custom_client_test.go),
  [`Example_rebuildableClient`](examples/example_rebuildable_client_test.go),
  [`Example_customBuilder`](examples/example_custom_builder_test.go)
- Configuration and transport: [`Example_configFromYAML`](examples/example_config_yaml_test.go),
  [`Example_refreshableConfig`](examples/example_refreshable_config_test.go),
  [`Example_buildHTTPClient`](examples/example_build_http_client_test.go),
  [`Example_customTransport`](examples/example_custom_transport_test.go)
- Middleware, errors, and retries: [`Example_middlewareOrdering`](examples/example_middleware_ordering_test.go),
  [`Example_errorDecoding`](examples/example_error_decoding_test.go),
  [`Example_conjureErrors`](examples/example_conjure_errors_test.go),
  [`Example_retriesAndBackoff`](examples/example_retries_backoff_test.go)

## Core concepts

### Runtime

`Runtime` is a one-method interface — the behavior callers need, not the pieces:

```go
type Runtime interface {
    Send(ctx context.Context, req *http.Request, opts SendOptions) (*http.Response, error)
}
```

`Call.Execute` builds a *path-only* request plus a `SendOptions` and calls
`Send`. You can also call `Send` directly for low-level requests; see
[`Example_sendLowLevel`](examples/example_send_low_level_test.go) and
[`Example_sendRequestValues`](examples/example_send_request_values_test.go).
The request URL must be **relative** — only its path and query are used (the
runtime supplies scheme/host/port from the selected base URL per attempt); a
non-relative URL is rejected with `ErrNonRelativeRequestURL` rather than silently
rewritten.

The standard runtime returned by `Builder.Build` owns the request loop: base-URL
selection (it prepends a selected base URL to the path-only request on each
attempt), retries, per-attempt timeout, QoS redirect handling, telemetry, and
builder auth/header decoration. `SendOptions` carries per-request decoration
(`Values`), middlewares, and call-policy overrides (`Policy`). A custom runtime
— a test fake or a wrapper — implements the single `Send` method.

`RebuildableRuntime[B]` extends `Runtime` with a `Builder()` method that returns
a new builder seeded with the client's current settings, allowing reconfiguration
without starting from scratch. See
[`Example_rebuildableClient`](examples/example_rebuildable_client_test.go).

### Endpoints and Calls

An endpoint descriptor defines the HTTP method, a Conjure-style path template (e.g.
`/items/{itemId}`), and the typed codec. It is split into two types so body presence is
enforced by the type system:

- `BodyEndpoint[Req, Resp]` — methods that send a body (POST/PUT/PATCH). Its
  `Call(body Req)` takes the request body, so a body endpoint cannot be executed
  without one.
- `NoBodyEndpoint[Resp]` — methods that send no body (GET/DELETE/HEAD). Its `Call()`
  takes no argument.

Both are copy-on-write — every method returns a new value — so descriptors are safe to
store as package-level vars and reuse concurrently. Both produce a per-invocation
`Call[Resp]`, which owns the body, filled path params, and per-call overrides, and which
carries `Execute`.

**Constructors:**

| Constructor | Returns | Use case |
|-------------|---------|----------|
| `NewGET[Resp]` | `NoBodyEndpoint[Resp]` | GET, no body |
| `NewDELETE[Resp]` | `NoBodyEndpoint[Resp]` | DELETE, no body |
| `NewHEAD[Resp]` | `NoBodyEndpoint[Resp]` | HEAD, no body |
| `NewNoBodyEndpoint[Resp]` | `NoBodyEndpoint[Resp]` | any bodyless method |
| `NewPOST[Req, Resp]` | `BodyEndpoint[Req, Resp]` | POST with body |
| `NewPUT[Req, Resp]` | `BodyEndpoint[Req, Resp]` | PUT with body |
| `NewPATCH[Req, Resp]` | `BodyEndpoint[Req, Resp]` | PATCH with body |
| `NewBodyEndpoint[Req, Resp]` | `BodyEndpoint[Req, Resp]` | any method with a body |

A POST/PUT/PATCH/DELETE that intentionally sends *no* body uses `NewNoBodyEndpoint`
(there is no `WithNoBody`).

**Descriptor configuration** (each returns a new descriptor):

- `WithEncoder(BodyEncoder[Req])` (BodyEndpoint only) -- request body serialization
- `WithDecoder(BodyDecoder[Resp])` -- response body deserialization
- `WithAccept(string)` -- sets the Accept header
- `WithJSON()` -- sugar: JSON decoder + `Accept: application/json` (and, on a
  BodyEndpoint, a JSON request encoder)
- plus the `RequestOverrides` methods below, as **static defaults** for the RPC

**Path parameters** are filled per call on the `Call` via `WithPathParam`, using named
replacement in Conjure-style templates. Greedy parameters (`{param*}`) preserve slashes
while escaping each segment. See
[`Example_pathAndQueryParams`](examples/example_path_and_query_params_test.go) and
[`Example_greedyPathParam`](examples/example_greedy_path_param_test.go).

**Execution** — derive a `Call` from the descriptor, configure it, and execute:

```go
// Body endpoint — Call takes the body:
resp, httpResp, err := createItem.Call(requestBody).Execute(ctx, client)

// No-body endpoint — Call takes no argument:
resp, httpResp, err := getItem.Call().WithPathParam("itemId", id).Execute(ctx, client)
```

### Overrides

`Overrides` is a standalone copy-on-write value holding per-request configuration:
headers, query params, timeout, error decoder, basic auth, middleware, and buffer
pool. Generated service clients typically embed an `Overrides` and merge it into
every call via `Call.WithOverrides`; see [the service-client example](example_service_test.go).

The endpoint descriptors (`BodyEndpoint`/`NoBodyEndpoint`), the per-call `Call`, and
`Overrides` all implement the `RequestOverrides[D]` interface:

- `WithHeader(key, value, additionalValues...)` -- replaces all values for `key`
- `WithAddedHeader(key, value, additionalValues...)` -- appends one or more values
- `WithQuery(key, value, additionalValues...)` -- replaces all values for `key`
- `WithAddedQuery(key, value, additionalValues...)` -- appends one or more values
- `WithAddedQueryValues(url.Values)` -- bulk append from a `url.Values` map
- `WithTimeout(time.Duration)` -- per-attempt timeout (use a ctx deadline for total)
- `WithUnlimitedTimeout()` -- disable the per-attempt timeout (also `WithTimeout(0)`)
- `WithDefaultTimeout()` -- clear an inherited timeout so the client timeout applies
- `WithErrorDecoder(ErrorDecoder)` -- per-call error decoder. For a Conjure
  typed-error registry, use the free function
  `conjureerrors.WithConjureErrorDecoder(d, ced)` (in the `httpc/conjureerrors`
  sub-package, so this interface stays free of `conjure-go-contract/errors`)
- `WithNoErrorDecoder()` -- skip error decoding; `Execute` returns the raw response
- `WithDefaultErrorDecoder()` -- clear an inherited decoder so `DefaultErrorDecoder` applies
- `WithBasicAuth(user, pw)` -- per-call basic auth (see [Auth precedence](#auth-precedence))
- `WithDefaultBasicAuth()` -- clear per-call basic auth so lower-priority auth / an explicit header wins
- `WithMiddleware(Middleware)` -- append a per-request middleware (runs per attempt)
- `WithBufferPool(bytesbuffers.Pool)` -- per-call buffer pool for encoders (nil clears it)
- `WithDefaultBufferPool()` -- clear an inherited buffer pool

The `WithDefault*` methods clear this layer's scalar (when a descriptor or a lower
override layer set) so the lower/default behavior applies, rather than only
replacing it. What "default" means is per-scalar: `WithDefaultTimeout` falls back
to the client/runtime timeout; `WithDefaultErrorDecoder` falls back to
`DefaultErrorDecoder()` (there is no client decoder); `WithDefaultBasicAuth` drops
the per-call credential so lower-priority auth or an explicit `Authorization`
header applies; `WithDefaultBufferPool` encodes without a pool.
`WithUnlimitedTimeout` / `WithNoErrorDecoder` are the two explicit "off" states.

These methods serve two configuration layers that compose at execute time:

- On a **descriptor** (`BodyEndpoint`/`NoBodyEndpoint`), they set **static defaults**
  baked into the package-level descriptor — useful for headers or middlewares that are
  part of the RPC's definition.
- On a **`Call`** (or an `Overrides` merged into one via `Call.WithOverrides`), they
  capture **per-invocation values** — useful for headers derived from the call site
  context. A `Call` seeds from the descriptor's defaults, then per-call values win.

When per-invocation values combine with the descriptor defaults (and when an `Overrides`
is merged in):
- Headers and query params are **additive** across both layers
- The scalars (timeout, error decoder, basic auth, buffer pool) use **last-wins**: the
  per-call layer wins for any scalar it set, including an explicit clear via
  `WithDefault*` (a scalar the per-call layer never set leaves the descriptor default)
- Middlewares are **appended** (descriptor defaults first, then per-call)

### Codecs

Built-in encoders and decoders cover common content types:

**Encoders** (`BodyEncoder[Req]`):

| Function | Content-Type | Retryable | Notes |
|----------|-------------|-----------|-------|
| `JSONEncoder[Req]()` | `application/json` | Yes | Uses the [`WithBufferPool`](#overrides) pool if set |
| `BinaryEncoder(ct)` | caller-specified | If file can be reopened | Probes for `Stat()` and reopens named files for replay |
| `BinaryEncoderOnce(ct)` | caller-specified | No | Single-use stream; Content-Length -1, no `GetBody` |
| `BinaryEncoderWithReplay(ct)` | caller-specified | Yes | Takes `func() (io.ReadCloser, error)` |
| `GZIPEncoder[Req](inner)` | preserved | If inner is | Wraps any encoder with gzip |
| `SnappyEncoder[Req](inner)` | preserved | If inner is | Wraps any encoder with snappy |
| `ZLIBEncoder[Req](inner)` | preserved | If inner is | Wraps any encoder with deflate |

**Decoders** (`BodyDecoder[Resp]`):

| Function | Resp type | Notes |
|----------|-----------|-------|
| `JSONDecoder[Resp]()` | `Resp` | Deserializes JSON |
| `OptionalJSONDecoder[Resp]()` | `*Resp` | Returns nil on 204 No Content |
| `VoidDecoder()` | `struct{}` | Discards body |
| `BinaryDecoder()` | `io.ReadCloser` | Caller must close |
| `OptionalBinaryDecoder()` | `io.ReadCloser` | Returns nil on 204 No Content |

For the common JSON case, prefer the [`WithJSON()`](#endpoints-and-calls) shortcut on
an endpoint descriptor — it wires the JSON decoder and `Accept` (and, on a
`BodyEndpoint`, the JSON request encoder) in one call, rather than setting
`JSONEncoder`/`JSONDecoder` by hand.

Custom encoders and decoders can be created via `NewBodyEncoderFunc` and
`NewBodyDecoderFunc`; see [`Example_customCodec`](examples/example_custom_codec_test.go).
For binary and replayable streaming bodies, see
[`Example_binaryStreaming`](examples/example_binary_streaming_test.go) and
[`Example_replayableStreamingBody`](examples/example_replayable_streaming_body_test.go).

## Building clients

### Builder

`Builder` is the concrete builder implementing `BuilderAPI[B]`,
which composes `DialerBuilder`, `TLSConfigBuilder`, `TransportBuilder`, and
`ServiceBuilder`. All setters are mutable (modify the receiver) and return the
builder for chaining. Use `Clone()` to fork an independent copy.

```go
client, err := httpc.NewBuilder().
    SetServiceName("my-service").
    SetBaseURLs("https://host1.example.com", "https://host2.example.com").
    SetAuthToken(bearerToken).
    SetTimeout(30 * time.Second).
    SetMaxIdleConnsPerHost(50).
    Build(ctx)
```

### Refreshable configuration

Most settings support both static and refreshable variants. Refreshable setters
link the builder to a dynamic source that updates without rebuilding the client;
`Clone()` preserves those links, while a static setter replaces the refreshable
with a fixed value. See [`Example_refreshableConfig`](examples/example_refreshable_config_test.go).

### TLS client certificates

Client certificate files are watched by default and trigger TLS config rebuilds when
their contents change. For clients that need to re-read the cert/key files on every TLS
handshake, enable dynamic reload. See
[`Example_tlsCertificates`](examples/example_tls_certificates_test.go) and
[`Example_tlsEscapeHatch`](examples/example_tls_escape_hatch_test.go).

### YAML configuration

`ApplyConfig` and `ApplyConfigRefreshable` apply a `ClientConfig` struct (typically
unmarshaled from YAML/JSON) to the builder. This covers URIs, auth, timeouts, proxy,
TLS, retry, and metrics. See
[`Example_configFromYAML`](examples/example_config_yaml_test.go) and
[`Example_refreshableConfig`](examples/example_refreshable_config_test.go).

### Composable Params

`Param[B]` is a function type `func(B) B` for packaging reusable configuration:

```go
func WithMyDefaults[B httpc.ServiceBuilder[B]]() httpc.Param[B] {
    return func(b B) B {
        return b.SetTimeout(30 * time.Second).SetMaxAttempts(new(5))
    }
}

builder.Apply(WithMyDefaults[*httpc.Builder]())
```

Convenience constructors `Param0`, `Param1`, `Param2`, and `ParamVarArgs` build
`Param` values from setter methods without writing closures by hand.

### Builder hierarchy

The builder is decomposed into focused interfaces. `BuilderAPI` composes
them; `*Builder` is the concrete implementation that satisfies all four:

- **`DialerBuilder[B]`** -- TCP dial timeout, keep-alive, SOCKS proxy. Exposes
  `SetDialer(ContextDialer)` to inject a caller-provided dialer, and
  `BuildDialer(ctx)` to produce one standalone.
- **`TLSConfigBuilder[B]`** -- TLS config, CAs, client certs, InsecureSkipVerify.
  Exposes `SetTLSConfig(*tls.Config)` to inject a caller-provided config, and
  `BuildTLSConfig(ctx)` to produce one standalone.
- **`TransportBuilder[B]`** -- Connection pool sizes, HTTP/2, proxy, idle
  timeouts. Exposes `SetTransport(http.RoundTripper)` to inject a custom
  transport, and `BuildTransport(ctx)` to produce one standalone.
- **`ServiceBuilder[B]`** -- Service name, URIs, auth, middleware, retry,
  metrics, tracing, error handling. Exposes `Build(ctx)` for the full client.
- **`BuilderAPI[B]`** -- Embeds all four sub-interfaces.

Each `Set<X>` override on the sub-interfaces short-circuits the corresponding
`Build<X>` and the matching `Set*`/`Add*` settings in that domain are ignored.
This makes the package usable in three modes: full client (`Build`), partial
client with caller-supplied sub-component (`SetDialer`/`SetTLSConfig`/`SetTransport`),
or just the sub-component on its own (`BuildDialer`/`BuildTLSConfig`/`BuildTransport`).

### Extending the builder

To add your own fields while keeping the fluent, leaf-typed chaining, embed
`*httpc.BuilderCore[*MyBuilder]` and provide a constructor (wiring `self` via
`httpc.NewBuilderCore`) plus a `Clone` (via `BuilderCore.CloneCoreFor`). Every base
setter is promoted and returns `*MyBuilder`, so base and custom setters interleave in
one chain, and the `RebuildableRuntime` from `Build` hands your concrete type back
from `Builder()` across rebuilds. See
[`Example_customBuilder`](examples/example_custom_builder_test.go).

## Middleware

`Middleware` wraps HTTP round-trips for cross-cutting concerns:

```go
type Middleware interface {
    RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error)
}
```

`MiddlewareFunc` is the function adapter. Middleware can be added at three levels:

1. **Builder outer** (`AddMiddleware`) -- runs inside telemetry, outside the inner middleware and the request-value decoration.
2. **Builder inner** (`AddInnerMiddleware`) -- runs inside the outer middleware, just outside the request-value decoration.
3. **Per-request** (`Call.WithMiddleware`, or as a descriptor/`Overrides` default) -- carried in `SendOptions.Middlewares` and applied by the runtime **innermost**, closest to the transport, so it can still override the resolved request (even auth) on the wire.

The full stack the standard runtime composes, outermost to innermost:

```
Runtime.Send: retry loop, per-attempt timeout, redirect handling
  each attempt:
  -> URI selector
  -> Telemetry (metrics + tracing + B3 trace headers + panic recovery)
  -> Builder outer middleware (AddMiddleware)
  -> Builder inner middleware (AddInnerMiddleware)
  -> Request-value decoration (builder auth/headers + descriptor/per-call headers & query)
  -> Per-request middleware (from the Call)
  -> http.Transport
```

The standard runtime applies the URI selector and builds a call-scoped
`*http.Client` per attempt. Every user middleware layer — builder and per-request
— runs inside telemetry with the resolved URL, so it is traced, metered, and
panic-recovered. Auth and header values are resolved per attempt as the
request-value decoration (see [Auth precedence](#auth-precedence)) just outside the
per-request middleware, so an imperative per-request middleware runs last and can
still override the resolved request on the wire.

(A custom `Runtime` receives `SendOptions.Middlewares` and `SendOptions.Values` in
`opts` and may apply, inspect via `RequestValues.Snapshot`, or ignore them — there
is no hidden seam to satisfy.)

Error decoding is **not** a middleware layer. It runs in `Call.Execute` after
`Send` returns the raw HTTP response (see [Error handling](#error-handling)).
For middleware examples, see
[`Example_middlewareOrdering`](examples/example_middleware_ordering_test.go),
[`Example_middlewareLogging`](examples/example_middleware_logging_test.go),
[`Example_middlewareRequestSigning`](examples/example_middleware_request_signing_test.go),
and [`Example_middlewareResponseRewrite`](examples/example_middleware_response_rewrite_test.go).

## Retry and URI selection

`Send` returns raw `(*http.Response, error)` with no error decoding. The retry
loop operates on response status codes and transport errors directly, following the
[Conjure QoS protocol](https://github.com/palantir/http-remoting#quality-of-service-retry-failover-throttling).

Requests are retried when the request body is replayable (`GetBody` is set on the
`*http.Request`) and one of the following conditions is met:

- **Transport errors** (connection refused, DNS errors, EOF, etc.)
- **429 Too Many Requests** -- throttle; retried with exponential backoff
- **503 Service Unavailable** -- retried against a different host
- **307 / 308 Redirects** -- retried against the `Location` header target (these are
  QoS signals, not standard HTTP redirects; `Send` blocks the call-scoped `http.Client`
  from following them). Standard redirects (301/302/303) are still followed as usual.

Other status codes (including 4xx and non-503 5xx) are **not** retried.

- **Default attempts**: 2 per base URL (e.g. 2 URLs = 4 attempts)
- **`SetMaxAttempts(*int)`**: `nil` = default, `n > 0` = exactly n total attempts, `0` = unlimited
- **Backoff**: Exponential with jitter (initial: 250ms, max: 2s)
- **URI selection**: set via `SetURLSelector`. `BalancedURLSelector` (default) routes away from slow/erroring hosts; `RandomURLSelector` selects uniformly at random. Implement the `URLSelector` interface for a custom strategy.

See [`Example_retriesAndBackoff`](examples/example_retries_backoff_test.go),
[`Example_timeoutPerAttempt`](examples/example_timeout_per_attempt_test.go),
and [`Example_urlSelection`](examples/example_url_selection_test.go).

## Metrics

When metrics are enabled (via `SetMetrics`), the client emits detailed request instrumentation:

| Metric | Type | Description |
|--------|------|-------------|
| `client.response` | Timer (us) | Full round-trip duration |
| `client.request.in-flight` | Counter | Concurrent in-flight requests |
| `client.connection.create` | Counter | Connection acquisitions (tagged `reused:true/false`) |
| `client.connection.acquire` | Timer (us) | GetConn to GotConn latency |
| `client.time-to-first-byte` | Timer (us) | WroteRequest to GotFirstResponseByte |
| `client.dns.lookup` | Timer (us) | DNS resolution time |
| `client.tcp.connect` | Timer (us) | TCP dial time |
| `tls.handshake.attempt` | Meter | TLS handshake attempts |
| `tls.handshake` | Meter | Successful TLS handshakes |
| `tls.handshake.failure` | Meter | Failed TLS handshakes |
| `client.dns.lookup-error` | Meter | DNS resolution failures |
| `client.tcp.connect-error` | Meter | TCP dial failures |
| `client.connection.idle-return-error` | Meter | Pool saturation events |
| `client.request.write-error` | Meter | Request write failures |

Common tags: `service-name`, `method` (HTTP verb), `method-name` (RPC name from the endpoint),
`family` (1xx/2xx/3xx/4xx/5xx/timeout/other).
See [`Example_metrics`](examples/example_metrics_test.go).

## Error handling

By default, HTTP responses with status >= 307 are treated as errors. The built-in
error decoder attempts to unmarshal Conjure error bodies from JSON responses and
falls back to including the raw body text.
Use `StatusCodeFromError` and `LocationFromError` to inspect decoded errors.

Custom error decoders are set on a descriptor (static default for the RPC) or a
`Call`/`Overrides` (per-call), via `WithErrorDecoder`. The per-call decoder takes
priority; if neither is set, `Call.Execute` falls back to `DefaultErrorDecoder()`.
`WithDefaultErrorDecoder()` clears an inherited decoder so a call falls back to
`DefaultErrorDecoder()`.

To opt out of error decoding for a specific endpoint or call, use
`WithNoErrorDecoder()` (equivalently, set `NoErrorDecoder()`).
See [`Example_errorDecoding`](examples/example_error_decoding_test.go),
[`Example_conjureErrors`](examples/example_conjure_errors_test.go),
and [`Example_inspectRawErrors`](examples/example_inspect_raw_errors_test.go).

## Auth precedence

Multiple layers can set the `Authorization` header. From highest to lowest
priority on each request:

1. **Per-call basic auth** (`Call.WithBasicAuth`, or an `Overrides.WithBasicAuth`
   merged into the call) -- applied by `Call.Execute` after all `WithHeader` values
   are written, so it overrides any explicit `Authorization` header.
2. **Descriptor basic auth** (`BodyEndpoint`/`NoBodyEndpoint` `.WithBasicAuth`) --
   static basic auth baked into the descriptor. Same mechanism as (1); the per-call
   layer wins when both are set. `WithDefaultBasicAuth()` clears (1)/(2) so a lower
   layer applies.
3. **Per-call `WithHeader("Authorization", ...)`** -- explicit caller header.
   Wins over a descriptor's `WithHeader` for the same key.
4. **Descriptor `WithHeader("Authorization", ...)`** -- explicit static header.
5. **`Builder.SetBasicAuth` / `SetAuthToken` / `SetAuthTokenProvider` /
   `SetBasicAuthOptionalProvider` / `Set*Refreshable`** -- client-level
   middleware. Sets `Authorization` only when the header is still empty after
   layers (1)-(4), so it's the fallback for endpoints/calls that didn't set
   their own.

See [`Example_authPrecedence`](examples/example_auth_precedence_test.go).

## Tracing

Tracing is enabled by default using `witchcraft-go-tracing`:

- Each request creates a child span named after the endpoint's RPC name
- B3 trace headers (`X-B3-TraceId`, etc.) are propagated to downstream services
- `ContextWithForUserAgent(ctx, value)` propagates `For-User-Agent` unless the request
  already has that header set

Disable independently via `DisableTracing()` (spans) and
`DisableTraceHeaderPropagation()` (headers).
See [`Example_tracing`](examples/example_tracing_test.go).

## Service client pattern

Generated Conjure service clients should define package-level endpoint descriptors,
store an `httpc.Runtime` plus service-wide `httpc.Overrides`, and implement each RPC by
deriving a `Call` (with the body), filling path params, merging overrides, and calling
`Execute`. See [the service-client example](example_service_test.go).

## Concurrency

The endpoint descriptors (`BodyEndpoint`/`NoBodyEndpoint`), `Call`, and `Overrides` use
**copy-on-write** semantics: every method returns a new value without modifying the
original. Descriptors are safe to share across goroutines and store as package-level
variables; each invocation derives its own `Call`.

The runtime returned by `Build` (a `RebuildableRuntime`) is safe for concurrent use;
multiple goroutines may execute requests through it simultaneously.

`Builder` is **not** safe for concurrent use. All setter methods mutate the receiver.
To share a configuration across goroutines, call `Clone()` to create an independent
copy for each goroutine before mutating.

## Defaults

| Setting | Default |
|---------|---------|
| HTTP timeout | 60s |
| Dial timeout | 10s |
| Keep-alive | 30s |
| Idle connection timeout | 90s |
| TLS handshake timeout | 10s |
| Expect-Continue timeout | 1s |
| HTTP/2 read idle timeout | 30s |
| HTTP/2 ping timeout | 15s |
| Max idle connections | 200 |
| Max idle connections per host | 100 |
| Retry initial backoff | 250ms |
| Retry max backoff | 2s |
| Max attempts | 2 per base URL |

---
# TODOs

- Advertise deadline like dialogue
- Built-in multipart and form-urlencoded encoders
- Sticky Sessions
- `Node-Selection-Strategy` response header for Server-Driven Node-Selection Switching
- Unify limiter and scorer?
- Add metrics to limiter? (scores, queue lengths)
- **Lock retry/QoS semantics explicitly.** Dialogue is very specific: retryable QoS, 500 only for idempotent-ish methods, RetryHint.DO_NOT_RETRY, proxy attempt accounting, timeout
policy, and 308 only when Location exists. httpc currently documents transport/429/503/307/308 only, parses Retry-After but does not use it, and treats 307/308 without
Location as retry-next-host. I’d decide these before v1: validate RetryOther Location hosts, decide whether 307 is intentional Go legacy behavior, decide safe-method 500
retries, wire or remove Retry-After, and add retry-hint/proxy-attempt handling if Go services depend on it.
