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

package httpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"

	"github.com/palantir/pkg/refreshable/v2"
	"github.com/palantir/pkg/tlsconfig"
	werror "github.com/palantir/witchcraft-go-error"
)

// TLSConfigBuilder is an F-bounded interface for configuring TLS settings.
//
// Like all builders in this package, TLSConfigBuilder is mutable: setter methods modify
// the receiver and return it for chaining. Use Clone to fork an independent copy.
//
// Root CA configuration is additive: all Add* calls contribute certificates to a single
// pool. System CAs are included by default; call SetIncludeSystemCAs(false) to use only
// explicitly configured CAs.
//
// Client certificate configuration (for mutual TLS) uses Set* semantics — last write wins.
//
// SetTLSConfig is an escape hatch that replaces all other TLS settings with a
// caller-provided *tls.Config. When set, all other TLS builder methods are ignored.
//
// Changes to refreshable CA sources or watched certificate files trigger an automatic
// rebuild of the TLS configuration and underlying http.Transport.
//
// Generic functions can accept any TLSConfigBuilder and return the same concrete type:
//
//	func ConfigureMTLS[B TLSConfigBuilder[B]](b B) B {
//	    return b.
//	        AddCACertFiles("internal-ca.pem").
//	        SetClientCertFiles("client.key", "client.crt")
//	}
type TLSConfigBuilder[B TLSConfigBuilder[B]] interface {
	// Clone returns a deep copy of the builder. The copy is fully independent:
	// mutations to either the original or the clone do not affect the other.
	Clone() B

	// Apply applies the given Param functions to the builder in sequence.
	// Each Param may call setter methods to configure the builder.
	// Because builders are mutable, this modifies the receiver in place.
	Apply(...Param[B]) B

	// SetTLSConfig sets a complete TLS configuration, bypassing all other TLS settings.
	// When set, all Add* and Set* TLS methods are ignored. The provided config is cloned.
	SetTLSConfig(*tls.Config) B

	// SetInsecureSkipVerify controls whether the client verifies the server's certificate.
	SetInsecureSkipVerify(bool) B

	// Root CA configuration.
	// All sources are additive and combined into a single certificate pool.

	// SetIncludeSystemCAs controls whether the host system's trusted CA certificates
	// are included in the root CA pool. The default is true.
	SetIncludeSystemCAs(bool) B

	// AddCACertFiles adds CA certificates from the given PEM file paths.
	// Files are watched for changes; updates trigger a TLS config and transport rebuild.
	AddCACertFiles(...string) B

	// AddCACertBytes adds PEM-encoded CA certificate bytes to the pool.
	// A single PEM blob may contain multiple certificates.
	AddCACertBytes([]byte) B

	// AddCACertBytesRefreshable adds a refreshable source of PEM-encoded CA certificate bytes.
	// When the refreshable updates, the pool is rebuilt with the new certificates
	// from this source (combined with all other CA sources).
	AddCACertBytesRefreshable(refreshable.Refreshable[[][]byte]) B

	// AddCACerts adds parsed certificates to the pool.
	AddCACerts(...*x509.Certificate) B

	// Client certificate for mutual TLS (last write wins).

	// SetClientCertFiles sets client certificate and key file paths for mutual TLS.
	SetClientCertFiles(keyFile, certFile string) B

	// SetClientCertBytes sets client certificate and key bytes for mutual TLS.
	SetClientCertBytes(keyBytes, certBytes []byte) B

	// SetDynamicCertReload controls whether client cert/key files are re-read on each TLS handshake.
	SetDynamicCertReload(bool) B
}

func (b *Builder) SetTLSConfig(cfg *tls.Config) *Builder {
	if cfg == nil {
		b.tlsConfig = nil
	} else {
		b.tlsConfig = cfg.Clone()
	}
	return b
}

func (b *Builder) SetInsecureSkipVerify(skip bool) *Builder {
	if b.tlsConfig != nil {
		b.tlsConfig.InsecureSkipVerify = skip
	}
	b.tlsFileParams = refreshable.View(b.tlsFileParams, func(p tlsFileParams) tlsFileParams {
		p.InsecureSkipVerify = skip
		return p
	})
	return b
}

func (b *Builder) SetIncludeSystemCAs(include bool) *Builder {
	b.includeSystemCAs = include
	return b
}

func (b *Builder) AddCACertFiles(files ...string) *Builder {
	b.tlsFileParams = refreshable.View(b.tlsFileParams, func(p tlsFileParams) tlsFileParams {
		p.CAFiles = append(p.CAFiles, files...)
		return p
	})
	return b
}

func (b *Builder) AddCACertBytes(certBytes []byte) *Builder {
	b.caByteSlices = append(b.caByteSlices, certBytes)
	return b
}

func (b *Builder) AddCACertBytesRefreshable(r refreshable.Refreshable[[][]byte]) *Builder {
	if b.tlsCABytes != nil {
		existing := b.tlsCABytes
		b.tlsCABytes, _ = refreshable.Merge(existing, r, func(existingBytes, newBytes [][]byte) [][]byte {
			merged := make([][]byte, 0, len(existingBytes)+len(newBytes))
			merged = append(merged, existingBytes...)
			merged = append(merged, newBytes...)
			return merged
		})
	} else {
		b.tlsCABytes = r
	}
	return b
}

func (b *Builder) AddCACerts(certs ...*x509.Certificate) *Builder {
	for _, cert := range certs {
		pemBlock := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
		b.caByteSlices = append(b.caByteSlices, pemBlock)
	}
	return b
}

func (b *Builder) SetClientCertFiles(keyFile, certFile string) *Builder {
	b.clientCertKey = nil
	b.clientCertCert = nil
	b.tlsFileParams = refreshable.View(b.tlsFileParams, func(p tlsFileParams) tlsFileParams {
		p.KeyFile = keyFile
		p.CertFile = certFile
		return p
	})
	return b
}

func (b *Builder) SetClientCertBytes(keyBytes, certBytes []byte) *Builder {
	b.clientCertKey = keyBytes
	b.clientCertCert = certBytes
	return b
}

func (b *Builder) SetDynamicCertReload(enabled bool) *Builder {
	b.tlsFileParams = refreshable.View(b.tlsFileParams, func(p tlsFileParams) tlsFileParams {
		p.DynamicCertReload = enabled
		return p
	})
	return b
}

// tlsFileParams holds the file-based and flag-based TLS settings
// that feed into BuildTLSConfig. Separated from transportParams so
// that non-TLS transport changes do not trigger TLS rebuilds.
type tlsFileParams struct {
	CAFiles            []string
	CertFile           string
	KeyFile            string
	InsecureSkipVerify bool
	DynamicCertReload  bool
}

// tlsParams contains the parameters needed to build a *tls.Config.
// Its fields must all be compatible with reflect.DeepEqual.
type tlsParams struct {
	CABytes            [][]byte
	CertFile           string
	KeyFile            string
	CertBytes          []byte // PEM client cert bytes; preferred over CertFile when non-nil.
	KeyBytes           []byte // PEM client key bytes; preferred over KeyFile when non-nil.
	InsecureSkipVerify bool
	IncludeSystemCAs   bool
	DynamicCertReload  bool
}

// newRefreshableTLSConfig evaluates the provided tlsParams and returns a Validated[*tls.Config] that will update the
// underlying *tls.Config when the tlsParams change.
// If the initial tlsParams are invalid, newRefreshableTLSConfig will return an error.
// If the updated tlsParams are invalid, the config will continue to use the previous value and log the error.
func newRefreshableTLSConfig(ctx context.Context, params refreshable.Validated[tlsParams]) (refreshable.Validated[*tls.Config], error) {
	r, _, err := refreshable.MapValidated(ctx, params, func(ctx context.Context, p tlsParams) (*tls.Config, error) {
		return newTLSConfig(ctx, p)
	})
	if err != nil {
		return nil, werror.WrapWithContextParams(ctx, err, "failed to build RefreshableTLSConfig")
	}
	return r, nil
}

// newTLSConfig returns a *tls.Config built from the provided tlsParams.
func newTLSConfig(ctx context.Context, p tlsParams) (*tls.Config, error) {
	var tlsClientParams []tlsconfig.ClientParam
	if p.IncludeSystemCAs && len(p.CABytes) > 0 {
		// System CAs + custom CAs: start with system pool, augment with custom.
		var opts []tlsconfig.CertPoolOption
		for _, ca := range p.CABytes {
			if len(ca) > 0 {
				opts = append(opts, tlsconfig.CertPoolOptionCABytes(ca))
			}
		}
		tlsClientParams = append(tlsClientParams, tlsconfig.ClientRootCAs(func() (*x509.CertPool, error) {
			pool, err := x509.SystemCertPool()
			if err != nil {
				return nil, err
			}
			return tlsconfig.AugmentCertPoolWithCertPoolOptions(pool, opts)()
		}))
	} else if p.IncludeSystemCAs {
		// System CAs only, no custom CAs.
		tlsClientParams = append(tlsClientParams, tlsconfig.ClientRootCAs(
			func() (*x509.CertPool, error) { return x509.SystemCertPool() },
		))
	} else if len(p.CABytes) > 0 {
		// Custom CAs only (explicit opt-out from system CAs).
		var certPoolOptions []tlsconfig.CertPoolOption
		for _, ca := range p.CABytes {
			if len(ca) > 0 {
				certPoolOptions = append(certPoolOptions, tlsconfig.CertPoolOptionCABytes(ca))
			}
		}
		tlsClientParams = append(tlsClientParams, tlsconfig.ClientRootCAs(tlsconfig.CertPoolFromCertPoolOptions(certPoolOptions)))
	}
	// else: no ClientRootCAs — Go default uses system CAs.
	if len(p.CertBytes) > 0 && len(p.KeyBytes) > 0 {
		certBytes, keyBytes := p.CertBytes, p.KeyBytes
		tlsClientParams = append(tlsClientParams, tlsconfig.ClientKeyPair(func() (tls.Certificate, error) {
			return tls.X509KeyPair(certBytes, keyBytes)
		}))
	} else if p.CertFile != "" && p.KeyFile != "" {
		if p.DynamicCertReload {
			tlsClientParams = append(tlsClientParams, tlsconfig.ClientKeyPair(tlsconfig.TLSCertFromFiles(p.CertFile, p.KeyFile)))
		} else {
			tlsClientParams = append(tlsClientParams, tlsconfig.ClientKeyPairFiles(p.CertFile, p.KeyFile))
		}
	}
	if p.InsecureSkipVerify {
		tlsClientParams = append(tlsClientParams, tlsconfig.ClientInsecureSkipVerify())
	}
	tlsCfg, err := tlsconfig.NewClientConfig(tlsClientParams...)
	if err != nil {
		return nil, werror.WrapWithContextParams(ctx, err, "failed to build tlsConfig")
	}
	return tlsCfg, nil
}

// BuildTLSConfig builds the configured TLS configuration from the builder's TLS parameters.
// The returned Validated[*tls.Config] automatically rebuilds when CA files, refreshable
// CA byte sources, or transport TLS parameters change.
//
// If SetTLSConfig was called, the injected config is returned as a static validated refreshable.
// BuildTLSConfig returns an error if CA files cannot be read or system CAs cannot be loaded.
func (b *Builder) BuildTLSConfig(ctx context.Context) (refreshable.Validated[*tls.Config], error) {
	if len(b.errs) > 0 {
		if len(b.errs) == 1 {
			return nil, werror.WrapWithContextParams(ctx, b.errs[0], "builder configuration errors")
		}
		return nil, werror.WrapWithContextParams(ctx, errors.Join(b.errs...), "builder configuration errors")
	}

	// Path 1: Escape hatch — use the provided *tls.Config directly.
	// The caller owns the config and is responsible for setting secure defaults.
	if b.tlsConfig != nil {
		r := refreshable.New(b.tlsConfig.Clone())
		v, _, err := refreshable.Validate(ctx, r, func(context.Context, *tls.Config) error { return nil })
		if err != nil {
			return nil, err
		}
		return v, nil
	}

	// File-based and refreshable CA sources — subscribe to tlsFileParams (not transportParams).
	// Watch CA files, cert file, and key file so that content changes trigger a TLS config rebuild.
	fileSlices, _ := refreshable.Map(b.tlsFileParams, func(t tlsFileParams) map[string]struct{} {
		m := map[string]struct{}{}
		for _, file := range t.CAFiles {
			m[file] = struct{}{}
		}
		if t.CertFile != "" {
			m[t.CertFile] = struct{}{}
		}
		if t.KeyFile != "" {
			m[t.KeyFile] = struct{}{}
		}
		return m
	})
	multiFileRefreshable := refreshable.NewMultiFileRefreshable(ctx, fileSlices)
	if _, err := multiFileRefreshable.Validation(); err != nil {
		return nil, werror.WrapWithContextParams(ctx, err, "failed to read TLS files")
	}
	includeSystemCAs := b.includeSystemCAs
	clientCertCert := b.clientCertCert
	clientCertKey := b.clientCertKey
	tlsP, _ := refreshable.MergeValidatedAndRefreshable(ctx, multiFileRefreshable, b.tlsFileParams, func(fileBytes map[string][]byte, tp tlsFileParams) tlsParams {
		var caBytes [][]byte
		for path, contents := range fileBytes {
			if path == tp.CertFile || path == tp.KeyFile {
				continue
			}
			caBytes = append(caBytes, contents)
		}
		params := tlsParams{
			CABytes:            caBytes,
			InsecureSkipVerify: tp.InsecureSkipVerify,
			IncludeSystemCAs:   includeSystemCAs,
			DynamicCertReload:  tp.DynamicCertReload,
		}
		if tp.DynamicCertReload {
			params.CertFile = tp.CertFile
			params.KeyFile = tp.KeyFile
		} else {
			// Pass cert/key as bytes when watched via file refreshable so that
			// content changes are detected by DeepEqual and trigger a TLS rebuild.
			if certBytes := fileBytes[tp.CertFile]; len(certBytes) > 0 {
				params.CertBytes = certBytes
			}
			if keyBytes := fileBytes[tp.KeyFile]; len(keyBytes) > 0 {
				params.KeyBytes = keyBytes
			}
			// Fall back to file paths if bytes aren't available (files not in watch set).
			if len(params.CertBytes) == 0 && len(params.KeyBytes) == 0 {
				params.CertFile = tp.CertFile
				params.KeyFile = tp.KeyFile
			}
		}
		// Fall back to static client cert bytes if no file-based cert was configured.
		if len(params.CertBytes) == 0 && len(params.KeyBytes) == 0 &&
			params.CertFile == "" && params.KeyFile == "" &&
			len(clientCertCert) > 0 && len(clientCertKey) > 0 {
			params.CertBytes = clientCertCert
			params.KeyBytes = clientCertKey
		}
		return params
	})

	// Merge static CA byte slices.
	if len(b.caByteSlices) > 0 {
		staticSlices := make([][]byte, len(b.caByteSlices))
		copy(staticSlices, b.caByteSlices)
		tlsP, _ = refreshable.MergeValidatedAndRefreshable(ctx, tlsP, refreshable.New(staticSlices), func(params tlsParams, statics [][]byte) tlsParams {
			params.CABytes = append(params.CABytes, statics...)
			return params
		})
	}

	// Merge refreshable CA bytes.
	if b.tlsCABytes != nil {
		tlsP, _ = refreshable.MergeValidatedAndRefreshable(ctx, tlsP, b.tlsCABytes, func(params tlsParams, caByteSlices [][]byte) tlsParams {
			params.CABytes = append(params.CABytes, caByteSlices...)
			return params
		})
	}

	return newRefreshableTLSConfig(ctx, tlsP)
}
