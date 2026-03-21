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
client, err := httpc.NewStandardClientBuilder().
    SetServiceName("my-service").
    SetBaseURLs("https://host1", "https://host2").
    SetAuthToken("token").
    SetTimeout(30 * time.Second).
    SetMaxAttempts(intPtr(4)). // see "Retry configuration" below
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
var getItem = httpc.NewGET[GetItemResp]("/api/v1/items/{itemId}", "GetItem").
    SetDecoder(httpc.JSONDecoder[GetItemResp]()).
    SetAccept("application/json")

var createItem = httpc.NewPOST[CreateReq, CreateResp]("/api/v1/items", "CreateItem").
    SetEncoder(httpc.JSONEncoder[CreateReq]()).
    SetDecoder(httpc.JSONDecoder[CreateResp]()).
    SetAccept("application/json")

// At call site:
resp, err := httpc.ExecuteVoid(ctx, client,
    getItem.WithPathParam("itemId", itemId))

resp, err := createItem.Execute(ctx, client, body)
```

Key differences:
- The HTTP method, path template, RPC name, encoder, and decoder are defined once
  in the endpoint descriptor rather than repeated at every call site.
- Path parameters are filled by name (`WithPathParam("itemId", id)`) with automatic
  URL escaping, instead of manual `url.PathEscape` + string concatenation.
- The response is returned as a typed value, not written to a pointer.
- `ExecuteVoid` is used for endpoints with no request body (avoids passing `struct{}{}`).

## Feature mapping

### Client construction params

| Old (`httpclient.With*`) | New (`builder.Set*` / etc.) | Notes |
|---|---|---|
| `WithServiceName(s)` | `SetServiceName(s)` | |
| `WithBaseURLs([]string{...})` | `SetBaseURLs(urls...)` | Variadic, not slice |
| `WithRefreshableBaseURLs(r)` | `SetBaseURLsRefreshable(r)` | |
| `WithAllowCreateWithEmptyURIs()` | `SetAllowCreateWithEmptyURIs(true)` | Now takes bool |
| `WithRandomURIScoring()` | `SetURIScoringStrategy(httpc.URIScoringRandom)` | |
| `WithHTTPTimeout(d)` | `SetTimeout(d)` | |
| `WithDialTimeout(d)` | `SetDialTimeout(d)` | |
| `WithIdleConnTimeout(d)` | `SetIdleConnTimeout(d)` | |
| `WithTLSHandshakeTimeout(d)` | `SetTLSHandshakeTimeout(d)` | |
| `WithExpectContinueTimeout(d)` | `SetExpectContinueTimeout(d)` | |
| `WithResponseHeaderTimeout(d)` | `SetResponseHeaderTimeout(d)` | |
| `WithMaxIdleConns(n)` | `SetMaxIdleConns(n)` | |
| `WithMaxIdleConnsPerHost(n)` | `SetMaxIdleConnsPerHost(n)` | |
| `WithKeepAlive(d)` | `SetKeepAlive(d)` | |
| `WithDisableKeepAlives()` | `DisableKeepAlives()` | |
| `WithDisableHTTP2()` | `DisableHTTP2()` | |
| `WithHTTP2ReadIdleTimeout(d)` | `SetHTTP2ReadIdleTimeout(d)` | |
| `WithHTTP2PingTimeout(d)` | `SetHTTP2PingTimeout(d)` | |
| `WithMaxRetries(n)` | `SetMaxAttempts(p)` | **Different semantics, see below** |
| `WithUnlimitedRetries()` | `SetMaxAttempts(intPtr(0))` | 0 = unlimited |
| `WithInitialBackoff(d)` | `SetInitialBackoff(d)` | |
| `WithMaxBackoff(d)` | `SetMaxBackoff(d)` | |
| `WithProxyFromEnvironment()` | `SetProxyFromEnvironment()` | |
| `WithProxyURL(s)` | `SetHTTPProxyURL(s)` or `SetSocksProxyURL(s)` | **Split by protocol** |
| `WithNoProxy()` | `SetNoProxy()` | |
| `WithTLSConfig(c)` | `SetTLSConfig(c)` | |
| `WithTLSCABytes(r)` | `AddCACertBytesRefreshable(r)` | Additive in new API |
| `WithCAFiles(files)` | `AddCACertFiles(files...)` | Additive in new API |
| `WithKeyAndCertFile(k, c)` | `SetClientCertFiles(k, c)` | |
| `WithTLSInsecureSkipVerify()` | `SetInsecureSkipVerify(true)` | Now takes bool |
| `WithMetrics(tags...)` | `SetMetrics(tags...)` | |
| `WithoutMetrics()` | `SetDisableMetrics(true)` | |
| `WithDisableTracing()` | `DisableTracing()` | |
| `WithDisableTraceHeaderPropagation()` | `DisableTraceHeaderPropagation()` | |
| `WithDisablePanicRecovery()` | `DisablePanicRecovery()` | |
| `WithDisableRestErrors()` | `DisableRestErrors()` | |
| `WithErrorDecoder(d)` | `SetErrorDecoder(d)` | |
| `WithMiddleware(m)` | `AddMiddleware(m)` | |
| `WithInnerMiddleware(m)` | `AddInnerMiddleware(m)` | |
| `WithAddHeader(k, v)` | `AddHeader(k, v)` | |
| `WithSetHeader(k, v)` | `SetHeader(k, v)` | |
| `WithUserAgent(s)` | `SetUserAgent(s)` | |
| `WithAuthToken(t)` | `SetAuthToken(t)` | |
| `WithAuthTokenProvider(p)` | `SetAuthTokenProvider(p)` | |
| `WithBasicAuth(u, p)` | `SetBasicAuth(u, p)` | |
| `WithBasicAuthProvider(p)` | `SetBasicAuthProvider(p)` | |
| `WithOverrideRequestHost(h)` | `SetOverrideRequestHost(h)` | |
| `WithBytesBufferPool(p)` | `SetBytesBufferPool(p)` | |
| `WithConfig(cfg)` | `ApplyConfig(ctx, cfg)` | Requires context now |
| `NewClientFromRefreshableConfig(...)` | `ApplyConfigRefreshable(ctx, r)` | Called on builder, then `Build` |

### Request params

| Old (`httpclient.With*`) | New (Endpoint / Overrides) | Notes |
|---|---|---|
| `WithRequestMethod(m)` | `NewEndpoint[...]`/`NewGET`/`NewPOST`/etc. | Set once at endpoint definition |
| `WithPath(p)` | Endpoint constructor `path` arg | Path template with `{param}` |
| `WithPathf(fmt, args...)` | `WithPathParam(key, value)` | Named replacement, not fmt |
| `WithHeader(k, v)` | `ep.WithHeader(k, v)` or `overrides.WithHeader(k, v)` | Copy-on-write |
| `WithQueryValues(q)` | `ep.WithQueryParam(k, v)` (per key-value) | |
| `WithRPCMethodName(n)` | Endpoint constructor `name` arg | Set once at definition |
| `WithJSONRequest(v)` | `SetEncoder(httpc.JSONEncoder[T]())` | Set once on endpoint |
| `WithJSONResponse(&v)` | `SetDecoder(httpc.JSONDecoder[T]())` | Returns typed value |
| `WithRequestBody(v, enc)` | `SetEncoder(customEncoder)` | |
| `WithResponseBody(&v, dec)` | `SetDecoder(customDecoder)` | |
| `WithRawRequestBody(rc)` | `SetEncoder(httpc.BinaryEncoder(ct))` | |
| `WithRawResponseBody()` | `SetDecoder(httpc.BinaryDecoder())` | |
| `WithBinaryRequestBody(rb)` | `SetEncoder(httpc.BinaryEncoder(ct))` | Probes for seek/stat |
| `WithCompressedRequest(v, c)` | `SetEncoder(httpc.ZLIBEncoder(inner))` | Wraps any encoder |
| `WithSnappyCompressedRequest(v, c)` | `SetEncoder(httpc.SnappyEncoder(inner))` | Wraps any encoder |
| `WithRequestTimeout(d)` | `ep.WithTimeout(d)` or `overrides.WithTimeout(d)` | |
| `WithRequestBasicAuth(u, p)` | `ep.WithBasicAuth(u, p)` or `overrides.WithBasicAuth(u, p)` | |
| `WithRequestErrorDecoder(d)` | `ep.WithErrorDecoder(d)` or `overrides.WithErrorDecoder(d)` | |

### Types

| Old | New | Notes |
|---|---|---|
| `httpclient.Client` | `httpc.Client` | Simplified to `Do(*http.Request) (*http.Response, error)` |
| `httpclient.Middleware` | `httpc.Middleware` | Same interface |
| `httpclient.MiddlewareFunc` | `httpc.MiddlewareFunc` | Same adapter |
| `httpclient.ErrorDecoder` | `httpc.ErrorDecoder` | Same interface |
| `httpclient.TagsProvider` | `httpc.TagsProvider` | Same interface |
| `httpclient.StaticTagsProvider` | `httpc.Tags` (a `map[string]string`) | Simpler type |
| `httpclient.TokenProvider` | `httpc.TokenProvider` | Same signature |
| `httpclient.BasicAuth` | `httpc.BasicAuth` | Same struct |
| `httpclient.BasicAuthProvider` | `httpc.BasicAuthProvider` | Same signature |
| `httpclient.ClientConfig` | `httpc.ClientConfig` (alias) | Same underlying type |
| `httpclient.RequestBody` | `httpc.BodyEncoder[T]` | See "Request bodies" |
| `httpclient.RequestParam` | `httpc.Overrides` / `Endpoint` methods | See "Making requests" |
| `httpclient.ClientParam` | `httpc.Param[*StandardClientBuilder]` | See "Params" |
| N/A | `httpc.Endpoint[Req, Resp]` | New concept |
| N/A | `httpc.Overrides` | New concept |
| N/A | `httpc.ConfigurableClient[B]` | New concept |
| `httpclient.BasicAuthOptionalProvider` | N/A | Use `SetBasicAuthRefreshable` with `*BasicAuth` |

### Error handling

| Old | New | Notes |
|---|---|---|
| `httpclient.StatusCodeFromError(err)` | `httpc.StatusCodeFromError(err)` | Same |
| `httpclient.LocationFromError(err)` | `httpc.LocationFromError(err)` | Same |

### Context utilities

| Old | New | Notes |
|---|---|---|
| `httpclient.ContextWithRPCMethodName(ctx, n)` | `httpc.ContextWithRPCMethodName(ctx, n)` | Same; rarely needed now |
| N/A | `httpc.RPCMethodName(ctx)` | New: reads RPC name from context |

## Gotchas and behavioral differences

### Retry configuration: MaxRetries vs MaxAttempts

The old `WithMaxRetries(n)` set the number of **retries**, so the total number of
attempts was `n + 1`. The new `SetMaxAttempts(*int)` sets the total number of
**attempts** directly.

| Old | New | Total attempts |
|-----|-----|----------------|
| `WithMaxRetries(2)` | `SetMaxAttempts(intPtr(3))` | 3 |
| `WithMaxRetries(0)` | `SetMaxAttempts(intPtr(1))` | 1 (no retries) |
| `WithUnlimitedRetries()` | `SetMaxAttempts(intPtr(0))` | Unlimited |
| _(default: 2 * len(URIs))_ | _(default: `nil` = 2 per URI)_ | Same |

The `SetMaxAttempts` parameter is a `*int`:
- `nil` (default): 2 attempts per base URL
- `intPtr(0)`: unlimited attempts
- `intPtr(n)`: exactly n total attempts

### Proxy configuration is split

The old `WithProxyURL(url)` accepted HTTP, HTTPS, and SOCKS5 proxy URLs in a
single param. The new API splits these:

- `SetHTTPProxyURL(url)` for `http://` and `https://` proxies
- `SetSocksProxyURL(url)` for `socks5://` proxies

Using the wrong setter for the protocol will result in a builder error.

### TLS CA configuration is additive

The old `WithCAFiles` and `WithTLSCABytes` each **replaced** the CA pool.
The new `AddCACertFiles`, `AddCACertBytes`, `AddCACertBytesRefreshable`, and
`AddCACerts` are all **additive** -- each call appends to the pool. Call
`AddSystemCAs()` explicitly if you want the system CA pool as a base (the old
package included system CAs by default).

### Response body is returned, not written to a pointer

The old API wrote decoded responses into a pointer passed via `WithJSONResponse(&result)`.
The new API returns the decoded value directly:

```go
// Old:
var result MyResp
_, err := client.Do(ctx, httpclient.WithJSONResponse(&result), ...)

// New:
result, err := endpoint.Execute(ctx, client, body)
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
base := httpc.Overrides{}.WithHeader("X-Tenant", "acme")
withTimeout := base.WithTimeout(5 * time.Second)
// base does NOT have the timeout; withTimeout does.
```

This is safe for concurrent use but requires understanding that the return value
must be captured (the receiver is unchanged).

### Builders are mutable

Unlike endpoints and overrides, builders modify the receiver:

```go
b := httpc.NewStandardClientBuilder()
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

### Metrics names use underscores

The old metrics used dots (e.g. `client.response`). The new metrics use underscores
(e.g. `client_response`). The new package also emits additional metrics not present
in the old package:

| New metric | Description |
|---|---|
| `client_conn_acquire` | Time from GetConn to GotConn |
| `client_time_to_first_byte` | Server processing time |
| `client_dns_lookup` | DNS resolution time |
| `client_dns_lookup_error` | DNS resolution failures |
| `client_tcp_connect` | TCP connection time |
| `client_tcp_connect_error` | TCP connection failures |
| `client_conn_idle_return_error` | Connection pool saturation |
| `client_request_write_error` | Request write failures |

### `WithQueryValues` replaced by repeated `WithQueryParam`

The old `WithQueryValues(url.Values{...})` set all query params at once. The new
API adds them one at a time via `WithQueryParam(key, value)`, which accumulates:

```go
// Old:
httpclient.WithQueryValues(url.Values{"page": {"1"}, "size": {"10"}})

// New:
ep.WithQueryParam("page", "1").WithQueryParam("size", "10")
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
   `httpc.NewStandardClientBuilder()...Build(ctx)`.

2. **Define endpoints**: Create package-level `Endpoint` vars for each RPC, setting
   method, path template, encoder, decoder, and accept header.

3. **Replace `client.Do` calls**: Replace inline `client.Do(ctx, params...)` with
   `endpoint.Execute(ctx, client, body)` or `httpc.ExecuteVoid(ctx, client, ep)`.

4. **Migrate request params to Overrides**: Convert per-request `WithHeader`,
   `WithRequestTimeout`, `WithRequestBasicAuth`, etc. to `Overrides` methods.

5. **Update retry configuration**: Convert `WithMaxRetries(n)` to
   `SetMaxAttempts(intPtr(n+1))`.

6. **Update proxy configuration**: Split `WithProxyURL` calls by protocol.

7. **Update TLS CA configuration**: Add `AddSystemCAs()` if you need system CAs,
   then add custom CAs via the `Add*` methods.

8. **Update metrics consumers**: Change metric names from dot-separated to
   underscore-separated.

9. **Update error handling**: `StatusCodeFromError` and `LocationFromError` have
   the same signatures but live in the `httpc` package.

10. **Update tests**: Replace `*http.Response` assertions with typed response assertions.
    The new `Client` interface (`Do(*http.Request) (*http.Response, error)`) is easy
    to mock or satisfy with a test `httptest.Server`.