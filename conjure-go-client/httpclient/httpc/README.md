# httpc

Package `httpc` provides a type-safe, endpoint-centric HTTP client for Go services.
Clients are configured via fluent builders and make requests through reusable `Endpoint`
descriptors that pair typed encoders/decoders with HTTP method and path templates.

If you are migrating from the `httpclient` parent package, see [MIGRATION.md](MIGRATION.md).

## Quick start

```go
// 1. Build a client.
client, err := httpc.NewBuilder().
    SetServiceName("item-service").
    SetBaseURLs("https://item-service.example.com").
    SetAuthToken(token).
    Build(ctx)

// 2. Define an endpoint (typically a package-level var).
var getItem = httpc.NewGET[GetItemResponse]("/api/v1/items/{itemId}", "GetItem").
    SetDecoder(httpc.JSONDecoder[GetItemResponse]()).
    SetAccept("application/json")

// 3. Execute.
resp, _, err := httpc.ExecuteVoid(ctx, client,
    getItem.WithPathParam("itemId", "item-42"))
```

## Core concepts

### Client

`Client` is the minimal transport interface. Its signature matches `*http.Client.Do`,
so a plain `*http.Client` satisfies it:

```go
type Client interface {
    Do(req *http.Request) (*http.Response, error)
}
```

A `Client` built via `Builder` handles base-URL selection, retries with
backoff, middleware, timeout enforcement, and URI scoring transparently.

`ConfigurableClient[B]` extends `Client` with a `Builder()` method that returns a
new builder seeded with the client's current settings, allowing reconfiguration without
starting from scratch:

```go
newClient, err := client.Builder().SetTimeout(5 * time.Second).Build(ctx)
```

### Endpoint

`Endpoint[Req, Resp]` is a copy-on-write request descriptor. It defines the HTTP method,
a Conjure-style path template (e.g. `/items/{itemId}`), a typed encoder for the request
body, and a typed decoder for the response body. Every method on `Endpoint` returns a new
value without modifying the original, making endpoints safe to store as package-level vars
and derive per-call variants concurrently.

**Constructors** with sensible defaults for the type parameters:

| Constructor | Req type | Use case |
|-------------|----------|----------|
| `NewGET[Resp]` | `struct{}` | GET with no body |
| `NewDELETE[Resp]` | `struct{}` | DELETE with no body |
| `NewHEAD[Resp]` | `struct{}` | HEAD with no body |
| `NewPOST[Req, Resp]` | caller-chosen | POST with body |
| `NewPUT[Req, Resp]` | caller-chosen | PUT with body |
| `NewPATCH[Req, Resp]` | caller-chosen | PATCH with body |
| `NewEndpoint[Req, Resp]` | caller-chosen | Any method |

**Configuration** (each returns a new `Endpoint`):

- `SetEncoder(BodyEncoder[Req])` -- serialization for request body
- `SetDecoder(BodyDecoder[Resp])` -- deserialization for response body
- `SetAccept(string)` -- sets the Accept header

**Path parameters** use named replacement in Conjure-style templates:

```go
var ep = httpc.NewGET[Resp]("/items/{itemId}/version/{version}", "GetItem")

// Parameters can be provided in any order.
ep.WithPathParam("version", 3).WithPathParam("itemId", "widget-1")
```

Greedy parameters (`{param*}`) preserve slashes while escaping each segment:

```go
var ep = httpc.NewGET[Resp]("/files/{filePath*}", "GetFile")
ep.WithPathParam("filePath", "dir/sub dir/file.txt")
// produces: /files/dir/sub%20dir/file.txt
```

**Execution:**

```go
// With a request body:
resp, httpResp, err := endpoint.Execute(ctx, client, requestBody)

// Without a request body (Req = struct{}):
resp, httpResp, err := httpc.ExecuteVoid(ctx, client, endpoint)
```

### Overrides

`Overrides` is a standalone copy-on-write value holding per-request configuration:
headers, query params, timeout, error decoder, basic auth, and middleware. Generated
service clients typically embed an `Overrides` and merge it into every endpoint call
via `WithOverrides`:

```go
type myServiceClient struct {
    client    httpc.Client
    overrides httpc.Overrides
}

func (c *myServiceClient) GetItem(ctx context.Context, id string) (Resp, error) {
    resp, _, err := httpc.ExecuteVoid(ctx, c.client,
        getItemEndpoint.
            WithPathParam("itemId", id).
            WithOverrides(c.overrides))
    return resp, err
}
```

Both `Endpoint` and `Overrides` implement the `RequestOverrides[D]` interface,
which provides: `AddHeader`, `SetHeader`, `AddQuery`, `SetQuery`, `WithTimeout`,
`WithErrorDecoder`, `WithBasicAuth`, and `WithMiddleware`.

When an `Overrides` is merged into an `Endpoint`:
- Headers and query params are **additive**
- Timeout, error decoder, and basic auth use **last-wins**
- Middlewares are **appended**

### Codecs

Built-in encoders and decoders cover common content types:

**Encoders** (`BodyEncoder[Req]`):

| Function | Content-Type | Retryable | Notes |
|----------|-------------|-----------|-------|
| `JSONEncoder[Req]()` | `application/json` | Yes | Uses buffer pool when available |
| `BinaryEncoder(ct)` | caller-specified | If seekable | Probes for `io.Seeker` and `Stat()` |
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

Custom encoders and decoders can be created via `NewBodyEncoderFunc` and `NewBodyDecoderFunc`.

## Building clients

### Builder

`Builder` is the concrete builder implementing `ClientBuilder[B]`,
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

Most settings support both static and refreshable variants. Refreshable setters link
the builder to a dynamic source that updates without rebuilding the client:

```go
builder.SetTimeoutRefreshable(timeoutRefreshable)
builder.SetBaseURLsRefreshable(urlsRefreshable)
builder.SetMaxAttemptsRefreshable(attemptsRefreshable)
```

`Clone()` preserves refreshable links (both copies observe the same source).
Calling a static setter (e.g. `SetTimeout`) replaces the refreshable with a fixed value.

### TLS client certificates

Client certificate files are watched by default and trigger TLS config rebuilds when
their contents change. For clients that need to re-read the cert/key files on every TLS
handshake, enable dynamic reload:

```go
builder.
    SetClientCertFiles("client.key", "client.crt").
    SetDynamicCertReload(true)
```

### YAML configuration

`ApplyConfig` and `ApplyConfigRefreshable` apply a `ClientConfig` struct (typically
unmarshaled from YAML/JSON) to the builder. This covers URIs, auth, timeouts, proxy,
TLS, retry, and metrics:

```go
builder.ApplyConfig(ctx, config)
// or for dynamic config:
builder.ApplyConfigRefreshable(ctx, configRefreshable)
```

### Composable Params

`Param[B]` is a function type `func(B) B` for packaging reusable configuration:

```go
func WithMyDefaults[B httpc.ServiceBuilder[B]]() httpc.Param[B] {
    return func(b B) B {
        return b.SetTimeout(30 * time.Second).SetMaxAttempts(intPtr(5))
    }
}

builder.Apply(WithMyDefaults[*httpc.Builder]())
```

Convenience constructors `Param0`, `Param1`, `Param2` help build `Param` values
from setter methods without writing closures by hand.

### Builder hierarchy

The builder is decomposed into focused interfaces for use in generic code:

- **`DialerBuilder[B]`** -- TCP dial timeout, keep-alive, SOCKS proxy
- **`TLSConfigBuilder[B]`** -- TLS config, CAs, client certs, InsecureSkipVerify
- **`TransportBuilder[B]`** -- Connection pool sizes, HTTP/2, proxy, idle timeouts
- **`ServiceBuilder[B]`** -- Everything above plus auth, middleware, retry, metrics, tracing
- **`ClientBuilder[B]`** -- Composes all of the above

`Builder` implements `ClientBuilder` and exposes additional methods for
building intermediate artifacts: `BuildDialer`, `BuildTLSConfig`, `BuildTransport`,
and `BuildHTTPClient`.

## Middleware

`Middleware` wraps HTTP round-trips for cross-cutting concerns:

```go
type Middleware interface {
    RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error)
}
```

`MiddlewareFunc` is the function adapter. Middleware can be added at three levels:

1. **Builder outer** (`AddMiddleware`) -- wraps all inner middleware; applied by the `Client`.
2. **Builder inner** (`AddInnerMiddleware`) -- runs closest to the transport, inside metrics and tracing.
3. **Per-request** (`Overrides.WithMiddleware` or `Endpoint.WithMiddleware`) -- applied by `Endpoint.Execute` before calling `Client.Do`.

The full middleware stack from outermost to innermost:

```
Recovery -> User outer middleware -> URI scorer
         -> Tracing -> Metrics -> Inner recovery -> User inner middleware
         -> http.Transport
```

Per-request middleware (from `Overrides`/`Endpoint`) wraps the `Client` itself, so it
sits outside the client's entire middleware stack.

Error decoding is **not** a middleware layer. It runs in `Endpoint.Execute` after
`Client.Do` returns the raw HTTP response (see [Error handling](#error-handling)).

## Retry and URI selection

`Client.Do` returns raw `(*http.Response, error)` with no error decoding. The retry
loop operates on response status codes and transport errors directly, following the
[Conjure QoS protocol](https://github.com/palantir/http-remoting#quality-of-service-retry-failover-throttling).

Requests are retried when the request body is replayable (`GetBody` is set on the
`*http.Request`) and one of the following conditions is met:

- **Transport errors** (connection refused, DNS errors, EOF, etc.)
- **429 Too Many Requests** -- throttle; retried with exponential backoff
- **503 Service Unavailable** -- retried against a different host
- **307 / 308 Redirects** -- retried against the `Location` header target (these are
  QoS signals, not standard HTTP redirects; `Client` blocks `http.Client` from
  following them automatically). Standard redirects (301/302/303) are still followed
  by `http.Client` as usual.

Other status codes (including 4xx and non-503 5xx) are **not** retried.

- **Default attempts**: 2 per base URL (e.g. 2 URLs = 4 attempts)
- **`SetMaxAttempts(*int)`**: `nil` = default, `n > 0` = exactly n total attempts, `0` = unlimited
- **Backoff**: Exponential with jitter (initial: 250ms, max: 2s)
- **URI scoring**: `URIScoringBalanced` (default) routes away from slow/erroring hosts; `URIScoringRandom` selects uniformly at random

## Metrics

When metrics are enabled (via `SetMetrics`), the client emits detailed request instrumentation:

| Metric | Type | Description |
|--------|------|-------------|
| `client_response` | Timer (us) | Full round-trip duration |
| `client_request_in_flight` | Counter | Concurrent in-flight requests |
| `client_connection_create` | Counter | Connection acquisitions (tagged `reused:true/false`) |
| `client_conn_acquire` | Timer (us) | GetConn to GotConn latency |
| `client_time_to_first_byte` | Timer (us) | WroteRequest to GotFirstResponseByte |
| `client_dns_lookup` | Timer (us) | DNS resolution time |
| `client_tcp_connect` | Timer (us) | TCP dial time |
| `tls_handshake_attempt` | Meter | TLS handshake attempts |
| `tls_handshake` | Meter | Successful TLS handshakes |
| `tls_handshake_failure` | Meter | Failed TLS handshakes |
| `client_dns_lookup_error` | Meter | DNS resolution failures |
| `client_tcp_connect_error` | Meter | TCP dial failures |
| `client_conn_idle_return_error` | Meter | Pool saturation events |
| `client_request_write_error` | Meter | Request write failures |

Common tags: `service_name`, `method` (HTTP verb), `method_name` (RPC name from Endpoint),
`family` (1xx/2xx/3xx/4xx/5xx/timeout/other).

## Error handling

By default, HTTP responses with status >= 307 are treated as errors. The built-in
error decoder attempts to unmarshal Conjure error bodies from JSON responses and
falls back to including the raw body text.

```go
// Extract status code from error:
if code, ok := httpc.StatusCodeFromError(err); ok { ... }

// Extract Location header from redirect error:
if loc, ok := httpc.LocationFromError(err); ok { ... }
```

Custom error decoders can be set at the builder level (`SetErrorDecoder`) or
per-request (`WithErrorDecoder` on `Endpoint` or `Overrides`). Per-request error
decoders take priority over the client-level decoder; if the per-request decoder
does not handle a response, the client-level decoder is consulted as a fallback.

Use `DisableRestErrors()` on the builder to disable the default error decoder entirely.

## Tracing

Tracing is enabled by default using `witchcraft-go-tracing`:

- Each request creates a child span named after the endpoint's RPC name
- B3 trace headers (`X-B3-TraceId`, etc.) are propagated to downstream services
- `ContextWithForUserAgent(ctx, value)` propagates `For-User-Agent` unless the request
  already has that header set

Disable independently via `DisableTracing()` (spans) and
`DisableTraceHeaderPropagation()` (headers).

## Service client pattern

The intended pattern for generated Conjure service clients:

```go
// Package-level endpoint descriptors (immutable, safe for concurrent use).
var (
    createItem = httpc.NewPOST[CreateReq, CreateResp]("/api/v1/items", "CreateItem").
        SetEncoder(httpc.JSONEncoder[CreateReq]()).
        SetDecoder(httpc.JSONDecoder[CreateResp]()).
        SetAccept("application/json")

    getItem = httpc.NewGET[GetItemResp]("/api/v1/items/{itemId}", "GetItem").
        SetDecoder(httpc.JSONDecoder[GetItemResp]()).
        SetAccept("application/json")

    deleteItem = httpc.NewDELETE[struct{}]("/api/v1/items/{itemId}", "DeleteItem").
        SetDecoder(httpc.VoidDecoder())
)

// Service client holds transport + per-client overrides.
type itemServiceClient struct {
    client    httpc.Client
    overrides httpc.Overrides
}

func NewItemServiceClient(client httpc.Client, params ...httpc.Param[*itemServiceClientBuilder]) ItemServiceClient {
    c := &itemServiceClient{client: client}
    b := &itemServiceClientBuilder{inner: c}
    for _, p := range params {
        p(b)
    }
    return c
}

// Each method fills path params, merges overrides, and executes.
func (c *itemServiceClient) CreateItem(ctx context.Context, req CreateReq) (CreateResp, error) {
    resp, _, err := createItem.
        WithOverrides(c.overrides).
        Execute(ctx, c.client, req)
    return resp, err
}

func (c *itemServiceClient) GetItem(ctx context.Context, id string) (GetItemResp, error) {
    resp, _, err := httpc.ExecuteVoid(ctx, c.client,
        getItem.
            WithPathParam("itemId", id).
            WithOverrides(c.overrides))
    return resp, err
}

func (c *itemServiceClient) DeleteItem(ctx context.Context, id string) error {
    _, _, err := httpc.ExecuteVoid(ctx, c.client,
        deleteItem.
            WithPathParam("itemId", id).
            WithOverrides(c.overrides))
    return err
}
```

## Concurrency

`Endpoint` and `Overrides` use **copy-on-write** semantics: every method returns a
new value without modifying the original. They are safe to share across goroutines
and store as package-level variables.

`Client` (the interface returned by `Build`) is safe for concurrent use. Multiple
goroutines may call `Do` simultaneously.

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
