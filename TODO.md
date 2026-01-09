# Plan: Fix InternalTLSParams for TestCABytesAndCAFileCombined

## Problem
The `client_builder.go` references `refreshingclient.InternalTLSParams` which doesn't exist in `tlsconfig.go`. This causes a build failure.

## Files to Modify
- `conjure-go-client/httpclient/internal/refreshingclient/tlsconfig.go`

## Changes Required

### 1. Add `InternalTLSParams` struct (after `TLSParams` on line 34)

```go
// InternalTLSParams is used internally by the httpclient builder to combine CA bytes
// from multiple sources (files and dynamic providers). It intentionally does not include
// CAFiles since file contents are read and converted to CABytes before creating this struct.
type InternalTLSParams struct {
	CABytes            [][]byte
	CertFile           string
	KeyFile            string
	InsecureSkipVerify bool
}
```

### 2. Add new `NewRefreshableTLSConfig` overload for `InternalTLSParams`

Rename the existing `NewRefreshableTLSConfig` that takes `TLSParams` or add a new version that accepts `InternalTLSParams`. The client_builder.go calls this with `InternalTLSParams` on line 142.

Option: Change the signature to accept `InternalTLSParams` instead:

```go
func NewRefreshableTLSConfig(ctx context.Context, params refreshable.Refreshable[InternalTLSParams]) (refreshable.Validated[*tls.Config], error) {
	r, _, err := refreshable.MapWithError(params, func(p InternalTLSParams) (*tls.Config, error) {
		return NewTLSConfigFromInternalParams(ctx, p)
	})
	if err != nil {
		return nil, werror.WrapWithContextParams(ctx, err, "failed to build RefreshableTLSConfig")
	}
	return r, nil
}
```

### 3. Add `NewTLSConfigFromInternalParams` function

```go
func NewTLSConfigFromInternalParams(ctx context.Context, p InternalTLSParams) (*tls.Config, error) {
	var tlsParams []tlsconfig.ClientParam
	if len(p.CABytes) > 0 {
		var certPoolOptions []tlsconfig.CertPoolOption
		for _, ca := range p.CABytes {
			certPoolOptions = append(certPoolOptions, tlsconfig.CertPoolOptionCABytes(ca))
		}
		tlsParams = append(tlsParams, tlsconfig.ClientRootCAs(tlsconfig.CertPoolFromCertPoolOptions(certPoolOptions)))
	}
	if p.CertFile != "" && p.KeyFile != "" {
		tlsParams = append(tlsParams, tlsconfig.ClientKeyPairFiles(p.CertFile, p.KeyFile))
	}
	if p.InsecureSkipVerify {
		tlsParams = append(tlsParams, tlsconfig.ClientInsecureSkipVerify())
	}
	tlsConfig, err := tlsconfig.NewClientConfig(tlsParams...)
	if err != nil {
		return nil, werror.WrapWithContextParams(ctx, err, "failed to build tlsConfig")
	}
	return tlsConfig, nil
}
```

## Verification
Run: `go test -v -run TestCABytesAndCAFileCombined ./conjure-go-client/httpclient/...`
