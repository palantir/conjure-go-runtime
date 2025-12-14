# Issue: RefreshableTransport does not rebuild when CA file contents change

## Problem Summary

When CA certificate files are modified on disk (e.g., a new certificate is appended to an existing CA file), the HTTP client's TLS configuration is not updated. The client continues to use the old certificate pool until a `TransportParams` change triggers a transport rebuild.

## Root Cause

The refresh chain for CA files has a gap:

1. `NewMultiFileRefreshable` watches CA files and updates when contents change ✓
2. `tlsParams` refreshable (merged from `TransportParams` + `multiFile`) updates when file contents change ✓
3. `RefreshableTLSConfig` updates its `*tls.Config` when `tlsParams` changes ✓
4. **`RefreshableTransport` only subscribes to `TransportParams`, NOT to `TLSProvider`** ✗

The issue is in `transport.go:52-56`:

```go
func NewRefreshableTransport(ctx context.Context, p refreshable.Refreshable[TransportParams], tlsProvider TLSProvider, dialer ContextDialer) http.RoundTripper {
    mapped, _ := refreshable.Map(p, func(p TransportParams) *http.Transport {
        return newTransport(ctx, p, tlsProvider, dialer)
    })
    return &RefreshableTransport{Refreshable: mapped}
}
```

The `refreshable.Map` only triggers `newTransport` when `TransportParams` changes. When CA file contents change:
- The `TLSProvider` (`RefreshableTLSConfig`) correctly updates its internal `*tls.Config`
- But `newTransport` is never called to create a new `*http.Transport` with the updated TLS config
- The old `*http.Transport` continues to be used with stale TLS settings

## Reproduction

See `TestCAUpdatesToTheSameFileProperlyWorks` in `client_builder_test.go`:

1. Create a CA file with "Test CA"
2. Create client with that CA file
3. Make request - sees "Test CA" ✓
4. Append "Test CA 3" to the same file
5. Wait for file refresh (2 seconds)
6. Make request - expects "Test CA" + "Test CA 3", but only sees "Test CA" ✗

## Fix Options

### Option 1: Subscribe to TLSProvider changes in RefreshableTransport

Modify `NewRefreshableTransport` to also listen to the `TLSProvider` for changes. This would require:

1. Change `TLSProvider` interface to expose a `Refreshable` or `Subscribe` method
2. Use `refreshable.Merge` to combine `TransportParams` and TLS config changes
3. Rebuild the transport when either changes

```go
func NewRefreshableTransport(ctx context.Context, p refreshable.Refreshable[TransportParams], tlsConfig refreshable.Refreshable[*tls.Config], dialer ContextDialer) http.RoundTripper {
    merged, _ := refreshable.Merge(p, tlsConfig, func(params TransportParams, tls *tls.Config) *http.Transport {
        return newTransportWithTLS(ctx, params, tls, dialer)
    })
    return &RefreshableTransport{Refreshable: merged}
}
```

**Pros:** Clean separation, transport rebuilds exactly when needed
**Cons:** Requires interface changes, more invasive refactor

### Option 2: Call GetTLSConfig on every request (lazy evaluation)

Instead of baking the `*tls.Config` into the `*http.Transport` at construction time, call `tlsProvider.GetTLSConfig()` on each request.

This would require a custom `http.RoundTripper` that wraps the transport and updates TLS config before each request.

**Pros:** Simple, no refresh subscription changes needed
**Cons:** Overhead on every request, may have thread-safety concerns with mutating transport

### Option 3: Merge TLS params into TransportParams refresh chain earlier

In `client_builder.go`, instead of passing a `TLSProvider` to `NewRefreshableTransport`, merge the TLS config refreshable with `TransportParams` so that any TLS change triggers a `TransportParams` update.

```go
// In client_builder.go
transportWithTLS, _ := refreshable.Merge(b.TransportParams, tlsConfig, func(t TransportParams, tls *tls.Config) TransportParamsWithTLS {
    return TransportParamsWithTLS{TransportParams: t, TLSConfig: tls}
})
transport := refreshingclient.NewRefreshableTransport(ctx, transportWithTLS, dialer)
```

**Pros:** Keeps the refresh logic in one place
**Cons:** Requires new type, changes to `NewRefreshableTransport` signature

### Option 4: Use a refreshable *tls.Config directly in the transport

Modify `newTransport` to accept a `refreshable.Refreshable[*tls.Config]` and subscribe to it, closing the old transport and creating a new one when TLS config changes.

**Pros:** Targeted fix
**Cons:** Transport lifecycle management complexity, potential connection disruption
