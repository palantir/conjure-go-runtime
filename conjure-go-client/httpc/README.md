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
  [`Example_authPrecedence`](examples/example_auth_precedence_test.go),
  [`Example_oauth2TokenSource`](examples/example_auth_oauth2_test.go)
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

The package has three kinds of value: a **client** (a `Runtime`, built once), reusable
**endpoint descriptors** (one per RPC, stored as package-level vars), and a
per-invocation **`Call`** — plus an `Overrides` bag for per-request configuration. The
diagram shows how they are produced and consumed; each row mirrors a step of the quick
start above.

```
── build a client ───────────────────────────────────────────────────────────
NewBuilder() ─▶ *Builder ─── Build(ctx) ──▶ RebuildableRuntime
                mutable;                    · is a Runtime: Send(ctx, req, opts)
                Clone() to fork             · Builder() re-forks the builder

── describe an RPC (store as a package-level var) ─────────────────────────────
NewGET / NewDELETE / NewHEAD  ─▶ NoBodyEndpoint ─┐  descriptor holds the codec
NewPOST / NewPUT / NewPATCH   ─▶ BodyEndpoint   ─┤  (BodyEncoder/BodyDecoder, via
                                                 │  WithJSON/WithEncoder/WithDecoder)
                                                 └  + static request defaults

── make a call ────────────────────────────────────────────────────────────────
descriptor.Call(body)   (BodyEndpoint)   ─┐
descriptor.Call()       (NoBodyEndpoint)  ─┼─▶ Call ── Execute(ctx, client) ──▶ Send
     Overrides ── WithOverrides(o) ───────┘              │
     (reusable per-request bag)          returns (Resp, *http.Response, error)

RequestOverrides[D] — WithHeader/WithQuery/WithTimeout/WithAuthorization/… — is
implemented by BodyEndpoint & NoBodyEndpoint (static defaults) and by Call &
Overrides (per-invocation values).
```

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
— a test fake or a wrapper — implements the single `Send` method; see
[`Example_runtimeWrapper`](examples/example_runtime_wrapper_test.go).

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

Both are copy-on-write (every method returns a new value) and produce a per-invocation
`Call[Resp]` that owns the body, filled path params, and per-call overrides, and carries
`Execute`.

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
while escaping each segment; a `.` or `..` path segment is rejected (error deferred to
`Execute`) so an untrusted value cannot climb the path. See
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
headers, query params, timeout, error decoder, authorization, middleware, and buffer
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
  typed-error registry, use the free function `httpc.WithConjureErrorDecoder(d, ced)`
  (a free function rather than an interface method, so this interface stays free of
  `conjure-go-contract/errors`)
- `WithNoErrorDecoder()` -- skip error decoding; `Execute` returns the raw response
- `WithDefaultErrorDecoder()` -- clear an inherited decoder so `DefaultErrorDecoder` applies
- `WithAuthorization(Authorizer)` -- per-call auth, e.g. `WithAuthorization(httpc.BasicCredentials(user, pw))` or `WithAuthorization(httpc.NoAuthorization())` to send none (see [Auth](#auth))
- `WithDefaultAuthorization()` -- clear per-call auth so lower-priority auth / an explicit header wins
- `WithMiddleware(Middleware)` -- append a per-request middleware (runs per attempt)
- `WithBufferPool(bytesbuffers.Pool)` -- per-call buffer pool for encoders (nil clears it)
- `WithDefaultBufferPool()` -- clear an inherited buffer pool

The `WithDefault*` methods clear this layer's scalar (when a descriptor or a lower
override layer set) so the lower/default behavior applies, rather than only
replacing it. What "default" means is per-scalar: `WithDefaultTimeout` falls back
to the client/runtime timeout; `WithDefaultErrorDecoder` falls back to
`DefaultErrorDecoder()` (there is no client decoder); `WithDefaultAuthorization` drops
the per-call authorizer so lower-priority auth or an explicit `Authorization`
header applies; `WithDefaultBufferPool` encodes without a pool.
`WithUnlimitedTimeout`, `WithNoErrorDecoder`, and
`WithAuthorization(NoAuthorization())` are the explicit "off" states.

These methods apply to the two layers introduced in [Core concepts](#core-concepts): on
a **descriptor** they bake **static defaults** into the package-level RPC; on a **`Call`**
(or an `Overrides` merged in via `Call.WithOverrides`) they capture **per-invocation
values**. A `Call` seeds from the descriptor's defaults, then the layers compose:

- Headers and query params are **additive** across both layers.
- Scalars (timeout, error decoder, authorization, buffer pool) are **last-wins**: the
  per-call layer wins for any scalar it set, including an explicit clear via
  `WithDefault*`; a scalar it never set keeps the descriptor default.
- Middlewares are **appended** (descriptor defaults first, then per-call).

### Codecs

Built-in encoders and decoders cover common content types:

**Encoders** (`BodyEncoder[Req]`):

| Function | Content-Type | Retryable | Notes |
|----------|-------------|-----------|-------|
| `JSONEncoder[Req]()` | `application/json` | Yes | Uses the [`WithBufferPool`](#overrides) pool if set |
| `FormURLEncoder()` | `application/x-www-form-urlencoded` | Yes | Encodes `url.Values` |
| `MultipartEncoder()` | `multipart/form-data` (+boundary) | If parts reproduce | Streamed; takes `func(*multipart.Writer) error`, re-run per attempt |
| `BinaryEncoder(ct)` | caller-specified | If file can be reopened | Probes for `Stat()` and reopens named files for replay |
| `BinaryEncoderOnce(ct)` | caller-specified | No | Single-use stream; Content-Length -1, no `GetBody` |
| `BinaryEncoderWithReplay(ct)` | caller-specified | Yes | Takes `func() (io.ReadCloser, error)` |
| `GZIPEncoder[Req](inner)` | preserved | If inner is | Wraps any encoder with gzip |
| `ZLIBEncoder[Req](inner)` | preserved | If inner is | Wraps any encoder with deflate |
| `CompressedEncoder[Req, W](inner, enc, newWriter)` | preserved | If inner is | Generic base for the above; wraps any `io.WriteCloser` compressor |
| `snappybody.SnappyEncoder[Req](inner)` | preserved | If inner is | Snappy; in the `httpc/snappybody` sub-package so `github.com/golang/snappy` stays out of core |

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
[`Example_replayableStreamingBody`](examples/example_replayable_streaming_body_test.go);
for the compression wrappers, [`Example_compressedBody`](examples/example_compressed_body_test.go);
for form and multipart bodies, [`Example_formURLEncoded`](examples/example_form_encoding_test.go) and
[`Example_multipartUpload`](examples/example_form_encoding_test.go).

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

The standard runtime builds one call-scoped `*http.Client` for the send (wrapping the
transport in the stack above) and reuses it across attempts; each attempt clones the
request and re-runs the decoration and middleware chain, so retries never duplicate
added values. Every user middleware layer — builder and per-request — runs inside
telemetry with the resolved URL, so it is traced, metered, and panic-recovered. Auth and header values are resolved per attempt as the
request-value decoration (see [Auth](#auth)) just outside the
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
  from following them). The relocation is honored only if its `Location` matches a
  configured base URL on scheme, host, port, and base path; an off-target `Location` is
  refused with `ErrInvalidRelocation` rather than followed, so a server cannot pivot the
  request onto an arbitrary host. Standard redirects (301/302/303) are still followed as
  usual, with the cross-host sensitive headers (`Authorization`, `Cookie`, …) stripped.

Other status codes (including 4xx and non-503 5xx) are **not** retried.

> **Retry replays the request as-is, gated on body replayability — not idempotency.** A
> *mutating* request (POST/PUT) with a replayable body can therefore execute more than
> once when an attempt fails (matching Conjure's RetryOther contract: "all request
> parameters and headers are maintained"). Where duplicate execution would be unsafe,
> make the operation idempotent (e.g. an idempotency-key header), cap attempts with
> `SetMaxAttempts(new(1))` / per-call `WithMaxAttempts(new(1))`, or use a single-use body
> encoder (e.g. `BinaryEncoderOnce`) so the body is not replayable.

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

Common tags: `service-name`, `method` (HTTP verb), `method-name` (RPC name from the
endpoint), and `family` — the outcome class: an HTTP status family (`1xx`/`2xx`/`3xx`/
`4xx`/`5xx`) or, taking precedence over the status, a transport-error class
(`dns_error`, `timeout`, `tls_verify_error`, `connection_error`), else `other`. Some
metrics carry extra tags: `reused` (connection acquisition), `network` (TCP connect),
and `cipher`/`next_protocol`/`tls_version` (TLS handshake).
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

## Auth

Auth is a single abstraction, `Authorizer`, that yields a full `Authorization`
header value (scheme included, so any token type works). Build one with a
constructor and install it on the builder with `SetAuth`, or override it per
request with `WithAuthorization`:

```go
b.SetAuth(httpc.BearerTokenProvider(provide))     // client-level
ep.WithAuthorization(httpc.BasicCredentials(u, p)) // descriptor default
call.WithAuthorization(httpc.NoAuthorization())    // this call: send none
```

Constructors: `BearerToken`, `BearerTokenProvider`, `RefreshableBearerToken`,
`BasicCredentials`, `BasicCredentialsProvider`, `OptionalBasicCredentials`,
`RefreshableBasicCredentials`. `SetAuthToken(t)` and `SetBasicAuth(u, p)` are kept
as sugar for `SetAuth(BearerToken(t))` / `SetAuth(BasicCredentials(u, p))`.
`AuthorizerFunc` adapts a plain `func(ctx) (string, error)`. For a
`golang.org/x/oauth2.TokenSource`, `oauth2auth.TokenSource(src)` returns an
`Authorizer` (in the `httpc/oauth2auth` sub-package, so `golang.org/x/oauth2` stays
out of core).

### Precedence

Auth resolves by **precedence**, not a runtime "set if absent" check: each layer's
`Authorization` contributors merge into one ordered set and the highest-precedence
*present* one wins. The winner is chosen before it runs, so a lower-priority
provider that loses is never invoked — a failing token provider cannot fail a
request that overrode it. From highest to lowest priority on each request:

1. **Per-call / descriptor authorizer** (`Call.WithAuthorization`, an
   `Overrides.WithAuthorization` merged into the call, or a descriptor
   `BodyEndpoint`/`NoBodyEndpoint` `.WithAuthorization`). This is a *scalar* slot
   emitted as the trailing `Authorization` contributor, so it beats an explicit
   `WithHeader("Authorization", ...)` in the same layer regardless of call order.
   The per-call layer wins over the descriptor when both are set.
2. **Explicit `Authorization` headers** -- `WithHeader("Authorization", ...)`
   (per-call beats descriptor), and, on the direct `Runtime.Send` path, the ordered
   `RequestValues.WithAuthorization(...)` contributor (positional later-wins).
3. **Builder authorizer** (`Builder.SetAuth` and its `SetAuthToken` / `SetBasicAuth`
   sugar) -- client-level fallback: wins only when no higher layer set `Authorization`.

Three states are distinct:

| Form | Meaning |
| ---- | ------- |
| `WithAuthorization(a)` / `SetAuth(a)` | authenticate with `a` |
| `WithAuthorization(NoAuthorization())` | **suppress** -- send no `Authorization` and do not fall through to lower layers |
| `WithDefaultAuthorization()`, or a nil `Authorizer` to `SetAuth` / `Overrides.WithAuthorization` | **clear** this layer back to the default so a lower layer applies |

An `Authorizer` that resolves to `("", nil)` — an empty `BearerToken`, or an
optional/refreshable provider yielding no current credential — behaves like
suppress when it wins: `Authorization` is left unset and lower layers do not apply.
One exception to the "nil clears" rule: `RequestValues.WithAuthorization(nil)` (the
direct `Send` path) is a **no-op**, not a clear, because that contributor list has
no scalar slot — use `NoAuthorization()` to suppress there.

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

- Endpoint descriptors (`BodyEndpoint`/`NoBodyEndpoint`), `Call`, and `Overrides` are
  **copy-on-write** — every method returns a new value — so they are safe to share
  across goroutines and store as package-level vars; each invocation derives its own `Call`.
- The runtime from `Build` (a `RebuildableRuntime`) is safe for concurrent use.
- `Builder` is **not**: its setters mutate the receiver, so call `Clone()` before sharing
  a configuration across goroutines.

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
