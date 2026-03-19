package httpc

import (
	"crypto/tls"
	"crypto/x509"

	"github.com/palantir/pkg/refreshable/v2"
)

// TLSConfigBuilder is an F-bounded interface for configuring TLS settings.
//
// Like all builders in this package, TLSConfigBuilder is mutable: setter methods modify
// the receiver and return it for chaining. Use Clone to fork an independent copy.
//
// Root CA configuration is additive: all Add* calls contribute certificates to a single
// pool. If no CAs are added and SetTLSConfig is not called, the pool is empty (server
// certificates are not verified unless AddSystemCAs is called or InsecureSkipVerify is set).
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
//	func ConfigureMTLS[Self TLSConfigBuilder[Self]](b Self) Self {
//	    return b.AddSystemCAs().
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

	// AddSystemCAs includes the host system's trusted CA certificates in the pool.
	AddSystemCAs() B

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
}
