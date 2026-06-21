# Migrating from httpclient to httpc

This guide covers migrating from the `conjure-go-client/httpclient` package to the
new `conjure-go-client/httpc` package.

## Overview of changes

The new package replaces the old package's `Client.Do(ctx, params...)` call pattern
with an **endpoint-centric** approach where typed endpoint descriptors
(`BodyEndpoint[Req, Resp]` / `NoBodyEndpoint[Resp]`) define the HTTP shape of each RPC at
the package level. Each invocation derives a per-call `Call` from the descriptor, and
per-call customization happens through copy-on-write method chaining rather than variadic
`RequestParam` functions.

For a runnable side-by-side migration, see
[`Example_migrationFromHTTPClient`](examples/example_migration_from_httpclient_test.go).

**Key architectural shifts:**

1. **Endpoints replace RequestParam** -- Instead of passing `WithRequestMethod`,
   `WithPath`, `WithJSONRequest`, `WithJSONResponse`, etc. as variadic params to
   `client.Do`, you define an endpoint descriptor with a typed encoder/decoder and call
   `endpoint.Call(body).Execute(ctx, client)` (body endpoints) or
   `endpoint.Call().Execute(ctx, client)` (no-body endpoints).

2. **Builders are mutable, descriptors/Calls are immutable** -- The old package used
   immutable `ClientParam`/`HTTPClientParam` option functions. The new package uses
   mutable fluent builders (`SetFoo` modifies the receiver) with explicit `Clone()`.
   Endpoint descriptors, `Call`, and `Overrides` are copy-on-write (value semantics).

3. **Generics throughout** -- Builders, endpoints, codecs, and params all use Go
   generics for type safety. The builder hierarchy uses self-typed generic
   interfaces so helper functions preserve concrete builder types.

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
- `Build(ctx)` returns a `RebuildableRuntime` that retains the builder for
  reconfiguration via `client.Builder()`.
- `SetBaseURLs` takes variadic strings, not a slice.

## Making requests

The old API built the HTTP method, path, codecs, and RPC name inline at each
`client.Do(ctx, params...)` call. The new API defines those pieces once in a
package-level endpoint descriptor; each call site derives a `Call` (with the body, for
body endpoints), fills path params, and calls `Execute(ctx, client)`.

See [`Example_migrationFromHTTPClient`](examples/example_migration_from_httpclient_test.go)
for the old-to-new shape, plus [`Example_basicGet`](examples/example_basic_get_test.go)
and [`Example_postJSON`](examples/example_post_json_test.go) for standalone GET
and POST examples.

Key differences:
- The HTTP method, path template, RPC name, encoder, and decoder are defined once
  in the endpoint descriptor rather than repeated at every call site.
- Path parameters are filled by name on the `Call` (`Call().WithPathParam("itemId", id)`)
  with automatic URL escaping, instead of manual `url.PathEscape` + string concatenation.
- The response is returned as a typed value, not written to a pointer.
- No-body endpoints (`NoBodyEndpoint[Resp]` from `NewGET`/`NewDELETE`/`NewHEAD`) derive a
  `Call` with `Call()` — no body argument; body endpoints use `Call(body)`.

## Gotchas and behavioral differences

### 5xx responses (except 503) are no longer retried

The old package ran error decoding inside the retry loop. This converted all error
responses (including 5xx) into Go errors with `resp = nil`, which triggered the
retrier's "nil response → retry" path. This meant all 5xx responses were retried,
not just 503.

The new package returns raw HTTP responses from `Send` and the retrier operates
on `resp.StatusCode` directly. Only the status codes specified by the
[Conjure QoS protocol](https://github.com/palantir/http-remoting#quality-of-service-retry-failover-throttling)
are retried: **429** (throttle), **503** (unavailable), **307/308** (redirect), and
transport errors (connection refused, DNS errors, etc.). A 500 Internal Server Error,
for example, is no longer retried.

If your service relied on the old behavior of retrying all 5xx, you may see different
failure modes during transient 500 errors. The correct fix is server-side: services
should return 503 for conditions where client retry is appropriate.

### Disabling error decoding

The old client-level `httpclient.WithDisableRestErrors()` turned off REST error
decoding for every call. In the new package error decoding is a per-endpoint/per-call
concern: use `WithNoErrorDecoder()` on the endpoint descriptor (or a per-call `Call`/
`Overrides`) so `Execute` returns the raw response for every status code. To instead drop a custom
decoder and fall back to the default, use `WithDefaultErrorDecoder()`.

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

### Client cert argument order: cert first, key second

The old `WithKeyAndCertFile(keyFile, certFile)` put **key** first. The new
`SetClientCertFiles(certFile, keyFile)` and `SetClientCertBytes(certBytes,
keyBytes)` match the Go standard library convention (`tls.LoadX509KeyPair` /
`tls.X509KeyPair` take cert first, key second). The legacy
`httpclient.WithKeyAndCertFile` wrapper keeps its existing signature and
flips internally for the bridge.

### Response body is returned, not written to a pointer

The old API wrote decoded responses into a pointer passed via `WithJSONResponse(&result)`.
The new API returns the decoded value directly from `Execute`; see
[`Example_migrationFromHTTPClient`](examples/example_migration_from_httpclient_test.go).

### No more `RequestBody` interface

The old package had a `RequestBody` interface with implementations like
`RequestBodyInMemory`, `RequestBodyStreamOnce`, and `RequestBodyStreamWithReplay`.
These are replaced by typed `BodyEncoder[T]` implementations:

| Old | New |
|-----|-----|
| `RequestBodyInMemory[T]` | Use `JSONEncoder` or a custom `BodyEncoder` |
| `RequestBodyStreamOnce[T]` | `BinaryEncoderOnce(contentType)` (non-retryable) or `BinaryEncoder(contentType)` (probes for `Stat`/`Seek`/`Name` and is retryable on `*os.File`) |
| `RequestBodyStreamWithReplay[T]` | `BinaryEncoderWithReplay(contentType)` |

See [`Example_binaryStreaming`](examples/example_binary_streaming_test.go) and
[`Example_replayableStreamingBody`](examples/example_replayable_streaming_body_test.go)
for streaming request bodies.

### Endpoint descriptor definitions are typically package-level vars

Unlike the old pattern where request configuration was built inline at every call site,
endpoints should be defined once as package-level variables. This is not just a style
preference -- it ensures the HTTP method, path template, codecs, and RPC name are
defined in one place and reused consistently.

### Copy-on-write semantics vs option functions

The old API used option functions (`WithFoo(value)`) that were applied once during
`client.Do`. The new API uses copy-on-write value types where each `With*` call
returns a new value:

```go
base := httpc.Overrides{}.WithAddedHeader("X-Tenant", "acme")
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

### The client contract is one method

The old `Client` interface had `Do`, `Get`, `Head`, `Post`, `Put`, `Delete` methods.
The new contract is `Runtime`, a single method:

```go
Send(ctx context.Context, req *http.Request, opts SendOptions) (*http.Response, error)
```

`Builder.Build` returns the standard implementation (a `RebuildableRuntime`) that owns
the retry/scoring/telemetry loop internally; `Call.Execute` drives requests through
it, and you can call `Send` directly for the low-level path. A test fake or wrapper
implements the single `Send` method. HTTP method selection happens at the endpoint level.

### `WithConfig` now requires context

The old `WithConfig(cfg)` was a `ClientParam` function. The new
`builder.ApplyConfig(ctx, cfg)` is called on the builder and requires a context
for validation error reporting.

### `WithBasicAuthOptionalProvider` → `SetBasicAuthOptionalProvider`

The old `WithBasicAuthOptionalProvider(func(ctx) (*BasicAuth, error))` is now
`SetBasicAuthOptionalProvider` on the builder. Same semantics: returning nil
skips setting the Authorization header for that request. There is also a
refreshable variant, `SetBasicAuthRefreshable(Refreshable[*BasicAuth])`, where
a nil current value disables auth.

### Additional metrics emitted

Metric names are unchanged from the old package, but several new metrics are
emitted that did not exist before. See [README.md](README.md#metrics) and
[`Example_metrics`](examples/example_metrics_test.go) for the full catalog; the
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

### `WithQueryValues` replaced by `WithQuery`/`WithAddedQuery`/`WithAddedQueryValues`

The old `WithQueryValues(url.Values{...})` set all query params at once. The new
API has `WithQuery(key, values...)` (replaces) and `WithAddedQuery(key, values...)`
(accumulates), plus `WithAddedQueryValues(url.Values)` for bulk. See
[`Example_pathAndQueryParams`](examples/example_path_and_query_params_test.go).

### Path construction uses named templates

The old API used `WithPath` and `WithPathf` with manual URL escaping. The new API
uses `{param}` placeholders (Conjure style) and `WithPathParam`, which escapes
values automatically. Greedy parameters (`{param*}`) preserve slashes while
escaping individual segments. See
[`Example_pathAndQueryParams`](examples/example_path_and_query_params_test.go) and
[`Example_greedyPathParam`](examples/example_greedy_path_param_test.go).

## Migration checklist

1. **Replace client construction**: Change `httpclient.NewClient*` calls to
   `httpc.NewBuilder()...Build(ctx)`.

2. **Define endpoints**: Create package-level descriptor vars for each RPC — a
   `BodyEndpoint` (`NewPOST`/`NewPUT`/`NewPATCH`) or `NoBodyEndpoint`
   (`NewGET`/`NewDELETE`/`NewHEAD`) — setting encoder, decoder, and accept (or `.WithJSON()`
   for all three at once).

3. **Replace `client.Do` calls**: For body-bearing RPCs, replace inline
   `client.Do(ctx, params...)` with `endpoint.Call(body).Execute(ctx, client)`.
   For body-less RPCs, call `endpoint.Call().Execute(ctx, client)` — no body argument.

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
    The new `Runtime` interface is a single method
    (`Send(ctx, *http.Request, SendOptions) (*http.Response, error)`), so it's easy to mock
    with a one-method fake or satisfy with a builder-built client over an `httptest.Server`.
