package httpc_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient/httpc"
	"github.com/palantir/pkg/refreshable/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildTLSConfig_EscapeHatch_PreservesUserConfig verifies that path 1 (SetTLSConfig)
// returns the user-provided config as-is. The caller owns the config and is responsible
// for setting secure defaults.
func TestBuildTLSConfig_EscapeHatch_PreservesUserConfig(t *testing.T) {
	tlsResult, err := httpc.NewStandardClientBuilder().
		SetTLSConfig(&tls.Config{MinVersion: tls.VersionTLS13}).
		BuildTLSConfig(context.Background())
	require.NoError(t, err)

	cfg, validErr := tlsResult.Validation()
	require.NoError(t, validErr)
	assert.Equal(t, uint16(tls.VersionTLS13), cfg.MinVersion, "escape hatch should preserve user's MinVersion")
}

// TestBuildTLSConfig_EscapeHatch_PreservesInsecureSkipVerify verifies that path 1
// preserves InsecureSkipVerify from the user-provided config.
func TestBuildTLSConfig_EscapeHatch_PreservesInsecureSkipVerify(t *testing.T) {
	tlsResult, err := httpc.NewStandardClientBuilder().
		SetTLSConfig(&tls.Config{InsecureSkipVerify: true}).
		BuildTLSConfig(context.Background())
	require.NoError(t, err)

	cfg, validErr := tlsResult.Validation()
	require.NoError(t, validErr)
	assert.True(t, cfg.InsecureSkipVerify)
}

// TestBuildTLSConfig_SystemCAs_SecureDefaults verifies that path 2 (AddSystemCAs)
// applies secure defaults via tlsconfig.NewClientConfig.
func TestBuildTLSConfig_SystemCAs_SecureDefaults(t *testing.T) {
	tlsResult, err := httpc.NewStandardClientBuilder().
		AddSystemCAs().
		BuildTLSConfig(context.Background())
	require.NoError(t, err)

	cfg, validErr := tlsResult.Validation()
	require.NoError(t, validErr)
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion, "system CAs path should enforce TLS 1.2 minimum")
	assert.NotEmpty(t, cfg.CipherSuites, "system CAs path should set cipher suites")
	assert.NotNil(t, cfg.RootCAs, "system CAs should be loaded")
}

// TestBuildTLSConfig_SystemCAs_InsecureSkipVerify verifies that SetInsecureSkipVerify
// is respected in path 2 (the bug fix — previously InsecureSkipVerify was ignored).
func TestBuildTLSConfig_SystemCAs_InsecureSkipVerify(t *testing.T) {
	tlsResult, err := httpc.NewStandardClientBuilder().
		AddSystemCAs().
		SetInsecureSkipVerify(true).
		BuildTLSConfig(context.Background())
	require.NoError(t, err)

	cfg, validErr := tlsResult.Validation()
	require.NoError(t, validErr)
	assert.True(t, cfg.InsecureSkipVerify, "InsecureSkipVerify should be respected in system CAs path")
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
}

// TestBuildTLSConfig_CertBytes_SecureDefaults verifies that path 2 with client cert
// bytes applies secure defaults.
func TestBuildTLSConfig_CertBytes_SecureDefaults(t *testing.T) {
	certPEM, keyPEM := generateTestKeyPair(t)

	tlsResult, err := httpc.NewStandardClientBuilder().
		SetClientCertBytes(keyPEM, certPEM).
		BuildTLSConfig(context.Background())
	require.NoError(t, err)

	cfg, validErr := tlsResult.Validation()
	require.NoError(t, validErr)
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion, "cert bytes path should enforce TLS 1.2 minimum")
	assert.NotEmpty(t, cfg.CipherSuites, "cert bytes path should set cipher suites")
}

// TestBuildTLSConfig_CertBytes_InsecureSkipVerify verifies that SetInsecureSkipVerify
// is respected when using SetClientCertBytes (path 2 bug fix).
func TestBuildTLSConfig_CertBytes_InsecureSkipVerify(t *testing.T) {
	certPEM, keyPEM := generateTestKeyPair(t)

	tlsResult, err := httpc.NewStandardClientBuilder().
		SetClientCertBytes(keyPEM, certPEM).
		SetInsecureSkipVerify(true).
		BuildTLSConfig(context.Background())
	require.NoError(t, err)

	cfg, validErr := tlsResult.Validation()
	require.NoError(t, validErr)
	assert.True(t, cfg.InsecureSkipVerify, "InsecureSkipVerify should be respected in cert bytes path")
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
}

// TestBuildTLSConfig_FileBased_SecureDefaults verifies that path 3 (file-based)
// produces configs with secure defaults (via refreshingclient.NewTLSConfig → tlsconfig.NewClientConfig).
func TestBuildTLSConfig_FileBased_SecureDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	caFile := filepath.Join(tmpDir, "ca.pem")
	writeTestCACertFile(t, caFile)

	tlsResult, err := httpc.NewStandardClientBuilder().
		AddCACertFiles(caFile).
		BuildTLSConfig(context.Background())
	require.NoError(t, err)

	cfg, validErr := tlsResult.Validation()
	require.NoError(t, validErr)
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion, "file-based path should enforce TLS 1.2 minimum")
	assert.NotEmpty(t, cfg.CipherSuites, "file-based path should set cipher suites")
}

// TestBuildTLSConfig_FileBased_InsecureSkipVerify verifies that path 3 respects
// SetInsecureSkipVerify.
func TestBuildTLSConfig_FileBased_InsecureSkipVerify(t *testing.T) {
	tlsResult, err := httpc.NewStandardClientBuilder().
		SetInsecureSkipVerify(true).
		BuildTLSConfig(context.Background())
	require.NoError(t, err)

	cfg, validErr := tlsResult.Validation()
	require.NoError(t, validErr)
	assert.True(t, cfg.InsecureSkipVerify)
}

// TestBuildTLSConfig_FileBased_CAFileRefresh verifies that when CA file contents
// change on disk, the TLS config is rebuilt with the new CA certificates.
func TestBuildTLSConfig_FileBased_CAFileRefresh(t *testing.T) {
	tmpDir := t.TempDir()
	caFile := filepath.Join(tmpDir, "ca.pem")
	writeTestCACertFile(t, caFile)

	tlsResult, err := httpc.NewStandardClientBuilder().
		AddCACertFiles(caFile).
		SetInsecureSkipVerify(true).
		BuildTLSConfig(context.Background())
	require.NoError(t, err)

	initialCfg, validErr := tlsResult.Validation()
	require.NoError(t, validErr)
	initialSubjects := initialCfg.RootCAs.Subjects()

	// Rewrite the CA file with a different certificate.
	writeTestCACertFileWithOrg(t, caFile, "Updated CA Org")

	// Wait for the file refreshable to pick up the change and rebuild the TLS config.
	waitForTLSConfigChange(t, tlsResult, func(cfg *tls.Config) bool {
		return len(cfg.RootCAs.Subjects()) > 0 && !subjectsEqual(cfg.RootCAs.Subjects(), initialSubjects)
	}, 10*time.Second)
}

// TestBuildTLSConfig_FileBased_CertFileRefresh verifies that when client cert/key
// files change on disk, the TLS config is rebuilt.
func TestBuildTLSConfig_FileBased_CertFileRefresh(t *testing.T) {
	tmpDir := t.TempDir()
	caFile := filepath.Join(tmpDir, "ca.pem")
	certFile := filepath.Join(tmpDir, "client.crt")
	keyFile := filepath.Join(tmpDir, "client.key")

	writeTestCACertFile(t, caFile)
	certPEM1, keyPEM1 := generateTestKeyPair(t)
	require.NoError(t, os.WriteFile(certFile, certPEM1, 0600))
	require.NoError(t, os.WriteFile(keyFile, keyPEM1, 0600))

	tlsResult, err := httpc.NewStandardClientBuilder().
		AddCACertFiles(caFile).
		SetClientCertFiles(keyFile, certFile).
		BuildTLSConfig(context.Background())
	require.NoError(t, err)

	initialCfg, validErr := tlsResult.Validation()
	require.NoError(t, validErr)
	require.NotNil(t, initialCfg)

	// Record the initial certificate serial number for comparison.
	initialCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	require.NoError(t, err)
	initialLeaf, err := x509.ParseCertificate(initialCert.Certificate[0])
	require.NoError(t, err)
	initialSerial := initialLeaf.SerialNumber

	// Write a new cert/key pair to the same files.
	certPEM2, keyPEM2 := generateTestKeyPair(t)
	require.NoError(t, os.WriteFile(certFile, certPEM2, 0600))
	require.NoError(t, os.WriteFile(keyFile, keyPEM2, 0600))

	// Wait for the TLS config to rebuild. The rebuilt config should contain the new cert
	// bytes, giving a different serial number.
	waitForTLSConfigChange(t, tlsResult, func(cfg *tls.Config) bool {
		// Cert/key bytes are passed via TLSParams.CertBytes/KeyBytes, which uses
		// ClientKeyPair (GetClientCertificate callback).
		if cfg.GetClientCertificate != nil {
			cert, err := cfg.GetClientCertificate(nil)
			if err != nil || cert == nil || len(cert.Certificate) == 0 {
				return false
			}
			leaf, err := x509.ParseCertificate(cert.Certificate[0])
			if err != nil {
				return false
			}
			return leaf.SerialNumber.Cmp(initialSerial) != 0
		}
		// Fallback: check cfg.Certificates directly.
		if len(cfg.Certificates) == 0 || len(cfg.Certificates[0].Certificate) == 0 {
			return false
		}
		leaf, err := x509.ParseCertificate(cfg.Certificates[0].Certificate[0])
		if err != nil {
			return false
		}
		return leaf.SerialNumber.Cmp(initialSerial) != 0
	}, 10*time.Second)
}

// TestBuildTLSConfig_NoTLSSettings verifies that path 3 with no TLS settings
// still produces a valid config with secure defaults.
func TestBuildTLSConfig_NoTLSSettings(t *testing.T) {
	tlsResult, err := httpc.NewStandardClientBuilder().
		BuildTLSConfig(context.Background())
	require.NoError(t, err)

	cfg, validErr := tlsResult.Validation()
	require.NoError(t, validErr)
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
}

// TestBuildTLSConfig_Clone_TLSIsolation verifies that cloning the builder
// isolates the escape-hatch TLS config between the original and clone.
func TestBuildTLSConfig_Clone_TLSIsolation(t *testing.T) {
	b1 := httpc.NewStandardClientBuilder().
		SetTLSConfig(&tls.Config{InsecureSkipVerify: true})

	b2 := b1.Clone()
	b2.SetTLSConfig(&tls.Config{InsecureSkipVerify: false})

	cfg1, err := b1.BuildTLSConfig(context.Background())
	require.NoError(t, err)
	cfg2, err := b2.BuildTLSConfig(context.Background())
	require.NoError(t, err)

	v1, _ := cfg1.Validation()
	v2, _ := cfg2.Validation()
	assert.True(t, v1.InsecureSkipVerify, "original should still be insecure")
	assert.False(t, v2.InsecureSkipVerify, "clone should be secure")
}

// --- helpers ---

// waitForTLSConfigChange polls the validated TLS config refreshable and waits until
// the predicate returns true or the timeout is reached.
func waitForTLSConfigChange(t *testing.T, v refreshable.Validated[*tls.Config], predicate func(*tls.Config) bool, timeout time.Duration) {
	t.Helper()
	// Poll-based approach: the file refreshable polls every second, so we check periodically.
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		cfg := v.Unvalidated()
		if predicate(cfg) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("timed out waiting for TLS config to update")
}

func writeTestCACertFile(t *testing.T, path string) {
	t.Helper()
	writeTestCACertFileWithOrg(t, path, "Test CA")
}

func writeTestCACertFileWithOrg(t *testing.T, path string, org string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			Organization: []string{org},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	require.NoError(t, err)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	require.NoError(t, os.WriteFile(path, certPEM, 0600))
}

func generateTestKeyPair(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	require.NoError(t, err)
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyDER, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

func subjectsEqual(a, b [][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if string(a[i]) != string(b[i]) {
			return false
		}
	}
	return true
}
