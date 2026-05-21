# Migrating from httpclient to httpc

This guide covers migrating from the `conjure-go-client/httpclient` package to the
new `conjure-go-client/httpclient/httpc` package.

## Overview of changes

The new package replaces the old package's `Client.Do(ctx, params...)` call pattern
with an **endpoint-centric** approach where typed `Endpoint[Req, Resp]` descriptors
define the HTTP shape of each RPC at the package level, and per-call customization
happens through copy-on-write method chaining rather than variadic `RequestParam` functions.

**Key architectural shifts:**

1. **Endpoint replaces RequestParam** -- Instead of passing `WithRequestMethod`,
   `WithPath`, `WithJSONRequest`, `WithJSONResponse`, etc. as variadic params to
   `client.Do`, you define an `Endpoint[Req, Resp]` with typed encoder/decoder and
   call `endpoint.Execute(ctx, client, body)`.

2. **Builders are mutable, endpoints are immutable** -- The old package used immutable
   `ClientParam`/`HTTPClientParam` option functions. The new package uses mutable
   fluent builders (`SetFoo` modifies the receiver) with explicit `Clone()`. Endpoints
   and Overrides remain copy-on-write (value semantics).

3. **Generics throughout** -- Builders, endpoints, codecs, and params all use Go
   generics for type safety. The builder hierarchy uses F-bounded polymorphism
   (`ServiceBuilder[B ServiceBuilder[B]]`) so generic helper functions preserve
   concrete types.

4. **Overrides replace RequestParam** -- Per-request configuration (headers, query
   params, timeout, auth, middleware, error decoder) uses `Overrides` values that
   compose via `WithOverrides`, replacing the old `With*` request param functions.

## Client construction

### Old

```go
client, err := httpclient.NewClientWithContext(ctx,
    httpclient.WithServiceName("my-service"),
    httpclient.WithBaseURLs([]string{"https://host1", "https://host2"}),
    httpclient.WithAuthToken("token"),
    httpclient.WithHTTPTimeout(30 * time.Second),
    httpclient.WithMaxRetries(3),
)
```

### New

```go
client, err := httpc.NewBuilder().
    SetServiceName("my-service").
    SetBaseURLs("https://host1", "https://host2").
    SetAuthToken("token").
    SetTimeout(30 * time.Second).
    SetMaxAttempts(new(4)). // see "Retry configuration" below
    Build(ctx)
```

Key differences:
- Variadic option functions are replaced by mutable builder methods.
- `Build(ctx)` returns a `ConfigurableClient` that retains the builder for
  reconfiguration via `client.Builder()`.
- `SetBaseURLs` takes variadic strings, not a slice.

## Making requests

### Old

```go
resp, err := client.Do(ctx,
    httpclient.WithRequestMethod(http.MethodGet),
    httpclient.WithPath("/api/v1/items/"+url.PathEscape(itemId)),
    httpclient.WithJSONResponse(&result),
    httpclient.WithRPCMethodName("GetItem"),
)
```

Or for a POST:

```go
resp, err := client.Do(ctx,
    httpclient.WithRequestMethod(http.MethodPost),
    httpclient.WithPath("/api/v1/items"),
    httpclient.WithJSONRequest(body),
    httpclient.WithJSONResponse(&result),
    httpclient.WithRPCMethodName("CreateItem"),
)
```

### New

```go
// Package-level endpoint definition (once per RPC).
var getItem = httpc.NewGET[GetItemResp]("GetItem", "/api/v1/items/{itemId}").
    SetDecoder(httpc.JSONDecoder[GetItemResp]()).
    SetAccept("application/json")

var createItem = httpc.NewPOST[CreateReq, CreateResp]("CreateItem", "/api/v1/items").
    SetEncoder(httpc.JSONEncoder[CreateReq]()).
    SetDecoder(httpc.JSONDecoder[CreateResp]()).
    SetAccept("application/json")

// At call site:
resp, _, err := getItem.WithPathParam("itemId", itemId).
    Execute(ctx, client, httpc.Void{})

resp, _, err := createItem.Execute(ctx, client, body)
```

Key differences:
- The HTTP method, path template, RPC name, encoder, and decoder are defined once
  in the endpoint descriptor rather than repeated at every call site.
- Path parameters are filled by name (`WithPathParam("itemId", id)`) with automatic
  URL escaping, instead of manual `url.PathEscape` + string concatenation.
- The response is returned as a typed value, not written to a pointer.
- `httpc.Void` is an alias for `struct{}`, used as the body argument for endpoints
  with no request body: `ep.Execute(ctx, client, httpc.Void{})`.

## Gotchas and behavioral differences

### 5xx responses (except 503) are no longer retried

The old package ran error decoding inside the retry loop. This converted all error
responses (including 5xx) into Go errors with `resp = nil`, which triggered the
retrier's "nil response → retry" path. This meant all 5xx responses were retried,
not just 503.

The new package returns raw HTTP responses from `Client.Do` and the retrier operates
on `resp.StatusCode` directly. Only the status codes specified by the
[Conjure QoS protocol](https://github.com/palantir/http-remoting#quality-of-service-retry-failover-throttling)
are retried: **429** (throttle), **503** (unavailable), **307/308** (redirect), and
transport errors (connection refused, DNS errors, etc.). A 500 Internal Server Error,
for example, is no longer retried.

If your service relied on the old behavior of retrying all 5xx, you may see different
failure modes during transient 500 errors. The correct fix is server-side: services
should return 503 for conditions where client retry is appropriate.

### Retry configuration: MaxRetries vs MaxAttempts

The old `WithMaxRetries(n)` set the number of **retries**, so the total number of
attempts was `n + 1`. The new `SetMaxAttempts(*int)` sets the total number of
**attempts** directly.

| Old | New | Total attempts |
|-----|-----|----------------|
| `WithMaxRetries(2)` | `SetMaxAttempts(new(3))` | 3 |
| `WithMaxRetries(0)` | `SetMaxAttempts(new(1))` | 1 (no retries) |
| `WithUnlimitedRetries()` | `SetMaxAttempts(new(0))` | Unlimited |
| _(default: 2 * len(URIs))_ | _(default: `nil` = 2 per URI)_ | Same |

The `SetMaxAttempts` parameter is a `*int`:
- `nil` (default): 2 attempts per base URL
- `new(0)`: unlimited attempts
- `new(n)`: exactly n total attempts

### Proxy configuration is split

The old `WithProxyURL(url)` accepted HTTP, HTTPS, and SOCKS5 proxy URLs in a
single param. The new API splits these:

- `SetHTTPProxyURL(url)` for `http://` and `https://` proxies
- `SetSocksProxyURL(url)` for `socks5://` proxies

Using the wrong setter for the protocol will result in a builder error.

### TLS CA configuration is additive

The old `WithCAFiles` and `WithTLSCABytes` each **replaced** the CA pool.
The new `AddCACertFiles`, `AddCACertBytes`, `AddCACertBytesRefreshable`, and
`AddCACerts` are all **additive** -- each call appends to the pool. Both
packages now include system CAs by default. Call `SetIncludeSystemCAs(false)`
to use only explicitly configured CAs.

### Response body is returned, not written to a pointer

The old API wrote decoded responses into a pointer passed via `WithJSONResponse(&result)`.
The new API returns the decoded value directly:

```go
// Old:
var result MyResp
_, err := client.Do(ctx, httpclient.WithJSONResponse(&result), ...)

// New:
result, _, err := endpoint.Execute(ctx, client, body)
```

### No more `RequestBody` interface

The old package had a `RequestBody` interface with implementations like
`RequestBodyInMemory`, `RequestBodyStreamOnce`, and `RequestBodyStreamWithReplay`.
These are replaced by typed `BodyEncoder[T]` implementations:

| Old | New |
|-----|-----|
| `RequestBodyInMemory[T]` | Just use `JSONEncoder` or custom `BodyEncoder` |
| `RequestBodyStreamOnce[T]` | `BinaryEncoder(contentType)` with non-seekable reader |
| `RequestBodyStreamWithReplay[T]` | `BinaryEncoderWithReplay(contentType)` |

### Endpoint definitions are typically package-level vars

Unlike the old pattern where request configuration was built inline at every call site,
endpoints should be defined once as package-level variables. This is not just a style
preference -- it ensures the HTTP method, path template, codecs, and RPC name are
defined in one place and reused consistently.

### Copy-on-write semantics vs option functions

The old API used option functions (`WithFoo(value)`) that were applied once during
`client.Do`. The new API uses copy-on-write value types where each `With*` call
returns a new value:

```go
base := httpc.Overrides{}.AddHeader("X-Tenant", "acme")
withTimeout := base.WithTimeout(5 * time.Second)
// base does NOT have the timeout; withTimeout does.
```

This is safe for concurrent use but requires understanding that the return value
must be captured (the receiver is unchanged).

### Builders are mutable

Unlike endpoints and overrides, builders modify the receiver:

```go
b := httpc.NewBuilder()
b.SetTimeout(30 * time.Second)  // modifies b in place
// Use b.Clone() if you need an independent copy.
```

### The Client interface is simpler

The old `Client` interface had `Do`, `Get`, `Head`, `Post`, `Put`, `Delete` methods.
The new `Client` has only `Do(*http.Request) (*http.Response, error)` -- the same
signature as `*http.Client.Do`. HTTP method selection happens at the endpoint level.

### `WithConfig` now requires context

The old `WithConfig(cfg)` was a `ClientParam` function. The new
`builder.ApplyConfig(ctx, cfg)` is called on the builder and requires a context
for validation error reporting.

### No `BasicAuthOptionalProvider`

The old `WithBasicAuthOptionalProvider(func(ctx) (*BasicAuth, error))` allowed
returning nil to skip auth. Use `SetBasicAuthRefreshable(Refreshable[*BasicAuth])`
in the new API, where a nil `*BasicAuth` disables auth.

### Additional metrics emitted

Metric names are unchanged from the old package, but several new metrics are
emitted that did not exist before. See README.md for the full catalog; the
additions are:

| Metric | Description |
|---|---|
| `client.connection.acquire` | Time from GetConn to GotConn |
| `client.time-to-first-byte` | WroteRequest to GotFirstResponseByte |
| `client.dns.lookup` | DNS resolution time |
| `client.dns.lookup-error` | DNS resolution failures |
| `client.tcp.connect` | TCP dial time |
| `client.tcp.connect-error` | TCP dial failures |
| `client.connection.idle-return-error` | Connection pool saturation |
| `client.request.write-error` | Request write failures |

### `WithQueryValues` replaced by `AddQuery`/`SetQuery`

The old `WithQueryValues(url.Values{...})` set all query params at once. The new
API adds them one at a time via `AddQuery(key, value)` (which accumulates) or
`SetQuery(key, value)` (which replaces):

```go
// Old:
httpclient.WithQueryValues(url.Values{"page": {"1"}, "size": {"10"}})

// New:
ep.AddQuery("page", "1").AddQuery("size", "10")
```

### Path construction uses named templates

The old API used `WithPath` and `WithPathf` with manual URL escaping:

```go
// Old:
httpclient.WithPathf("/api/v1/items/%s", url.PathEscape(itemId))

// New:
ep.WithPathParam("itemId", itemId)  // automatic escaping
```

Path templates use `{param}` placeholders (Conjure style). Greedy parameters
(`{param*}`) preserve slashes while escaping individual segments.

## Migration checklist

1. **Replace client construction**: Change `httpclient.NewClient*` calls to
   `httpc.NewBuilder()...Build(ctx)`.

2. **Define endpoints**: Create package-level `Endpoint` vars for each RPC, setting
   method, path template, encoder, decoder, and accept header.

3. **Replace `client.Do` calls**: Replace inline `client.Do(ctx, params...)` with
   `endpoint.Execute(ctx, client, body)`. Use `httpc.Void{}` as the body for
   endpoints with no request body.

4. **Migrate request params to Overrides**: Convert per-request `WithHeader`,
   `WithRequestTimeout`, `WithRequestBasicAuth`, etc. to `Overrides` methods.

5. **Update retry configuration**: Convert `WithMaxRetries(n)` to
   `SetMaxAttempts(new(n+1))`.

6. **Update proxy configuration**: Split `WithProxyURL` calls by protocol.

7. **Update TLS CA configuration**: CA configuration is now additive. Replace
   `WithCAFiles`/`WithTLSCABytes` with the corresponding `Add*` methods.

8. **Update error handling**: `StatusCodeFromError` and `LocationFromError` have
   the same signatures but live in the `httpc` package.

9. **Update tests**: Replace `*http.Response` assertions with typed response assertions.
    The new `Client` interface (`Do(*http.Request) (*http.Response, error)`) is easy
    to mock or satisfy with a test `httptest.Server`.
