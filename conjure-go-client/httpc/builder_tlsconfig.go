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
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"

	"github.com/palantir/pkg/refreshable/v2"
	"github.com/palantir/pkg/tlsconfig"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/palantir/witchcraft-go-logging/wlog/svclog/svc1log"
)

// TLSConfigBuilder configures TLS settings and is one slice of [BuilderAPI].
// Root CA configuration is additive across all Add* calls. System CAs are
// included by default; call SetIncludeSystemCAs(false) to use only explicit
// CAs. Client cert configuration uses last-write-wins Set* semantics.
// Changes to refreshable CA sources or watched cert/key files trigger an
// automatic TLS rebuild. SetTLSConfig is an escape hatch that replaces all
// other TLS settings — see [TLSConfigBuilder.BuildTLSConfig].
//
// A configured *Builder can produce a standalone *tls.Config via
// [Builder.BuildTLSConfig], or provide one for a full [Runtime] via
// [Builder.Build].
type TLSConfigBuilder[Self TLSConfigBuilder[Self]] interface {
	Clone() Self
	Apply(...Param[Self]) Self

	// SetTLSConfig installs a caller-provided *tls.Config (cloned).
	// [TLSConfigBuilder.BuildTLSConfig] returns this config as-is, skipping
	// CA/client-cert/InsecureSkipVerify construction. Pass nil to clear and
	// re-enable internal construction.
	SetTLSConfig(*tls.Config) Self
	// SetInsecureSkipVerify controls whether the client verifies the server's certificate.
	SetInsecureSkipVerify(bool) Self
	// SetIncludeSystemCAs controls inclusion of the host system's CA pool. Default: true.
	SetIncludeSystemCAs(bool) Self
	// AddCACertFiles adds CA certs from PEM file paths; files are watched for changes.
	AddCACertFiles(...string) Self
	// AddCACertBytes adds PEM-encoded CA cert bytes; each blob may contain
	// multiple certs.
	AddCACertBytes(...[]byte) Self
	// AddCACertBytesRefreshable adds a refreshable source of PEM-encoded CA cert bytes.
	AddCACertBytesRefreshable(refreshable.Refreshable[[][]byte]) Self
	// AddCACerts adds parsed certificates to the pool.
	AddCACerts(...*x509.Certificate) Self
	// SetClientCertFiles sets client cert and key file paths for mutual TLS.
	// Matches [tls.LoadX509KeyPair] argument order (cert first, key second).
	SetClientCertFiles(certFile, keyFile string) Self
	// SetClientCertBytes sets client cert and key bytes for mutual TLS.
	// Matches [tls.X509KeyPair] argument order (cert first, key second).
	SetClientCertBytes(certBytes, keyBytes []byte) Self
	// SetDynamicCertReload controls whether cert/key files are re-read on each handshake.
	SetDynamicCertReload(bool) Self

	// BuildTLSConfig returns the configured TLS config. If [TLSConfigBuilder.SetTLSConfig]
	// was called with a non-nil value, that config (cloned) is returned and the
	// CA / client-cert / InsecureSkipVerify settings are ignored. Otherwise the
	// config is built from those settings and rebuilds automatically when CA
	// files, refreshable CA byte sources, or TLS file params change.
	BuildTLSConfig(ctx context.Context) (refreshable.Validated[*tls.Config], error)
}

// SetTLSConfig replaces all TLS settings with the provided *tls.Config (cloned).
// When set, all Add* and Set* TLS methods on this builder are ignored.
func (b *BuilderCore[Self]) SetTLSConfig(cfg *tls.Config) Self {
	if cfg == nil {
		b.tlsConfig = nil
	} else {
		b.tlsConfig = cfg.Clone()
	}
	return b.self
}

// SetInsecureSkipVerify controls whether the client verifies the server's
// certificate. Ignored when [Builder.SetTLSConfig] installed an escape-hatch
// config (the caller owns InsecureSkipVerify on that config).
func (b *BuilderCore[Self]) SetInsecureSkipVerify(skip bool) Self {
	b.tlsFileParams = refreshable.View(b.tlsFileParams, func(p tlsFileParams) tlsFileParams {
		p.InsecureSkipVerify = skip
		return p
	})
	return b.self
}

// SetIncludeSystemCAs controls whether the host system's trusted CA certificates
// are included in the root CA pool. Default: true.
func (b *BuilderCore[Self]) SetIncludeSystemCAs(include bool) Self {
	b.includeSystemCAs = include
	return b.self
}

// AddCACertFiles adds CA certificates from PEM file paths. Files are watched for
// changes; updates trigger a TLS config and transport rebuild.
func (b *BuilderCore[Self]) AddCACertFiles(files ...string) Self {
	b.tlsFileParams = refreshable.View(b.tlsFileParams, func(p tlsFileParams) tlsFileParams {
		p.CAFiles = append(p.CAFiles, files...)
		return p
	})
	return b.self
}

// AddCACertBytes adds PEM-encoded CA certificate bytes to the pool. Each blob
// may contain multiple certificates.
func (b *BuilderCore[Self]) AddCACertBytes(certBytes ...[]byte) Self {
	b.caByteSlices = append(b.caByteSlices, certBytes...)
	return b.self
}

// AddCACertBytesRefreshable adds a refreshable source of PEM-encoded CA certificate bytes.
// When the refreshable updates, the pool is rebuilt with the new certificates combined
// with all other CA sources.
func (b *BuilderCore[Self]) AddCACertBytesRefreshable(r refreshable.Refreshable[[][]byte]) Self {
	if b.tlsCABytes != nil {
		b.tlsCABytes = refreshable.MergeAuto(b.tlsCABytes, r, func(existingBytes, newBytes [][]byte) [][]byte {
			merged := make([][]byte, 0, len(existingBytes)+len(newBytes))
			merged = append(merged, existingBytes...)
			merged = append(merged, newBytes...)
			return merged
		})
	} else {
		b.tlsCABytes = r
	}
	return b.self
}

// AddCACerts adds parsed certificates to the pool.
func (b *BuilderCore[Self]) AddCACerts(certs ...*x509.Certificate) Self {
	for _, cert := range certs {
		pemBlock := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
		b.caByteSlices = append(b.caByteSlices, pemBlock)
	}
	return b.self
}

// SetClientCertFiles sets client certificate and key file paths for mutual TLS.
// Argument order matches [tls.LoadX509KeyPair]: cert first, key second.
// Files are watched for changes; updates trigger a TLS config rebuild. For
// per-handshake re-reading of rotated files, also call SetDynamicCertReload(true).
func (b *BuilderCore[Self]) SetClientCertFiles(certFile, keyFile string) Self {
	b.clientCertKey = nil
	b.clientCertCert = nil
	b.tlsFileParams = refreshable.View(b.tlsFileParams, func(p tlsFileParams) tlsFileParams {
		p.KeyFile = keyFile
		p.CertFile = certFile
		return p
	})
	return b.self
}

// SetClientCertBytes sets client certificate and key bytes for mutual TLS.
// Argument order matches [tls.X509KeyPair]: cert first, key second.
func (b *BuilderCore[Self]) SetClientCertBytes(certBytes, keyBytes []byte) Self {
	b.clientCertKey = bytes.Clone(keyBytes)
	b.clientCertCert = bytes.Clone(certBytes)
	b.tlsFileParams = refreshable.View(b.tlsFileParams, func(p tlsFileParams) tlsFileParams {
		p.KeyFile = ""
		p.CertFile = ""
		return p
	})
	return b.self
}

// SetDynamicCertReload controls whether client cert/key files are re-read on each
// TLS handshake. Useful for environments with frequent cert rotation.
func (b *BuilderCore[Self]) SetDynamicCertReload(enabled bool) Self {
	b.tlsFileParams = refreshable.View(b.tlsFileParams, func(p tlsFileParams) tlsFileParams {
		p.DynamicCertReload = enabled
		return p
	})
	return b.self
}

// tlsFileParams holds file-based and flag-based TLS settings. Separated from
// transportParams so that non-TLS transport changes do not trigger TLS rebuilds.
type tlsFileParams struct {
	CAFiles            []string
	CertFile           string
	KeyFile            string
	InsecureSkipVerify bool
	DynamicCertReload  bool
}

// tlsParams feeds [newTLSConfig]. All fields must compare with reflect.DeepEqual.
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

// BuildTLSConfig returns the configured TLS config. If [Builder.SetTLSConfig]
// was called with a non-nil value, that config (cloned) is returned as a static
// validated refreshable, and the CA / client-cert / InsecureSkipVerify settings
// are ignored. Otherwise the config is built and rebuilds when CA files,
// refreshable CA byte sources, or TLS file params change. Errors if CA files
// cannot be read or system CAs cannot be loaded.
func (b *BuilderCore[Self]) BuildTLSConfig(ctx context.Context) (refreshable.Validated[*tls.Config], error) {
	// TLS construction is independent of base URLs, proxies, and retries; only a
	// failed config blocks it (CA-file errors surface inline below).
	if err := b.errs.joined(ctx, fieldConfig); err != nil {
		return nil, err
	}

	// Escape hatch: use the caller-provided *tls.Config directly.
	if b.tlsConfig != nil {
		r := refreshable.New(b.tlsConfig.Clone())
		return refreshable.ValidateAuto(ctx, r, func(context.Context, *tls.Config) error { return nil })
	}

	// Watch CA files plus the cert/key files so content changes trigger a rebuild.
	fileSlices := refreshable.MapAuto(b.tlsFileParams, func(t tlsFileParams) map[string]struct{} {
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
	tlsP := refreshable.MergeValidatedAndRefreshableAuto(ctx, multiFileRefreshable, b.tlsFileParams, newBaseTLSParamsMapper(includeSystemCAs, clientCertCert, clientCertKey))

	if len(b.caByteSlices) > 0 {
		staticSlices := make([][]byte, len(b.caByteSlices))
		copy(staticSlices, b.caByteSlices)
		tlsP = refreshable.MergeValidatedAndRefreshableAuto(ctx, tlsP, refreshable.New(staticSlices), func(params tlsParams, statics [][]byte) tlsParams {
			params.CABytes = append(params.CABytes, statics...)
			return params
		})
	}

	if b.tlsCABytes != nil {
		tlsP = refreshable.MergeValidatedAndRefreshableAuto(ctx, tlsP, b.tlsCABytes, func(params tlsParams, caByteSlices [][]byte) tlsParams {
			params.CABytes = append(params.CABytes, caByteSlices...)
			return params
		})
	}

	rebuild := false
	return refreshable.MapValidatedAuto(ctx, tlsP, func(ctx context.Context, p tlsParams) (*tls.Config, error) {
		if rebuild {
			svc1log.FromContext(ctx).Debug("Reconstructing TLS Config")
		} else {
			rebuild = true
		}
		return newTLSConfig(ctx, p)
	})
}

func newBaseTLSParamsMapper(includeSystemCAs bool, clientCertCert []byte, clientCertKey []byte) func(fileBytes map[string][]byte, tp tlsFileParams) tlsParams {
	return func(fileBytes map[string][]byte, tp tlsFileParams) tlsParams {
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
			// Pass cert/key as bytes so DeepEqual detects content changes and
			// triggers a TLS rebuild via the file refreshable.
			if certBytes := fileBytes[tp.CertFile]; len(certBytes) > 0 {
				params.CertBytes = certBytes
			}
			if keyBytes := fileBytes[tp.KeyFile]; len(keyBytes) > 0 {
				params.KeyBytes = keyBytes
			}
			// Fall back to file paths when bytes are unavailable.
			if len(params.CertBytes) == 0 && len(params.KeyBytes) == 0 {
				params.CertFile = tp.CertFile
				params.KeyFile = tp.KeyFile
			}
		}
		// Last-resort fallback to bytes provided via SetClientCertBytes.
		if len(params.CertBytes) == 0 && len(params.KeyBytes) == 0 &&
			params.CertFile == "" && params.KeyFile == "" &&
			len(clientCertCert) > 0 && len(clientCertKey) > 0 {
			params.CertBytes = clientCertCert
			params.KeyBytes = clientCertKey
		}
		return params
	}
}

// newTLSConfig builds a *tls.Config from p.
func newTLSConfig(ctx context.Context, p tlsParams) (*tls.Config, error) {
	var tlsClientParams []tlsconfig.ClientParam
	switch {
	case p.IncludeSystemCAs && len(p.CABytes) > 0:
		// System pool augmented with custom CAs.
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
	case p.IncludeSystemCAs:
		tlsClientParams = append(tlsClientParams, tlsconfig.ClientRootCAs(
			func() (*x509.CertPool, error) { return x509.SystemCertPool() },
		))
	case len(p.CABytes) > 0:
		var certPoolOptions []tlsconfig.CertPoolOption
		for _, ca := range p.CABytes {
			if len(ca) > 0 {
				certPoolOptions = append(certPoolOptions, tlsconfig.CertPoolOptionCABytes(ca))
			}
		}
		tlsClientParams = append(tlsClientParams, tlsconfig.ClientRootCAs(tlsconfig.CertPoolFromCertPoolOptions(certPoolOptions)))
	default:
		// Empty pool — suppress Go's implicit fallback to the system store.
		tlsClientParams = append(tlsClientParams, tlsconfig.ClientRootCAs(tlsconfig.CertPoolFromCertPoolOptions(nil)))
	}
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
