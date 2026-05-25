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

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/palantir/pkg/refreshable/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetTLSConfig_Nil verifies SetTLSConfig(nil) does not panic and clears any
// previously installed escape-hatch config.
func TestSetTLSConfig_Nil(t *testing.T) {
	b := httpc.NewBuilder().
		SetTLSConfig(&tls.Config{MinVersion: tls.VersionTLS13}).
		SetTLSConfig(nil)

	tlsResult, err := b.BuildTLSConfig(context.Background())
	require.NoError(t, err)

	cfg, validErr := tlsResult.Validation()
	require.NoError(t, validErr)
	// With escape-hatch cleared, normal path runs and produces a non-nil config.
	require.NotNil(t, cfg)
	assert.NotEqual(t, uint16(tls.VersionTLS13), cfg.MinVersion, "prior escape-hatch config should be cleared")
}

// TestBuildTLSConfig_EscapeHatch_PreservesUserConfig verifies that path 1 (SetTLSConfig)
// returns the user-provided config as-is. The caller owns the config and is responsible
// for setting secure defaults.
func TestBuildTLSConfig_EscapeHatch_PreservesUserConfig(t *testing.T) {
	tlsResult, err := httpc.NewBuilder().
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
	tlsResult, err := httpc.NewBuilder().
		SetTLSConfig(&tls.Config{InsecureSkipVerify: true}).
		BuildTLSConfig(context.Background())
	require.NoError(t, err)

	cfg, validErr := tlsResult.Validation()
	require.NoError(t, validErr)
	assert.True(t, cfg.InsecureSkipVerify)
}

// TestBuildTLSConfig_SystemCAs_SecureDefaults verifies that system CAs (now included
// by default) apply secure defaults via tlsconfig.NewClientConfig.
func TestBuildTLSConfig_SystemCAs_SecureDefaults(t *testing.T) {
	tlsResult, err := httpc.NewBuilder().
		BuildTLSConfig(context.Background())
	require.NoError(t, err)

	cfg, validErr := tlsResult.Validation()
	require.NoError(t, validErr)
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion, "system CAs path should enforce TLS 1.2 minimum")
	assert.NotEmpty(t, cfg.CipherSuites, "system CAs path should set cipher suites")
	assert.NotNil(t, cfg.RootCAs, "system CAs should be loaded")
}

// TestBuildTLSConfig_SystemCAs_InsecureSkipVerify verifies that SetInsecureSkipVerify
// is respected when system CAs are included (the default).
func TestBuildTLSConfig_SystemCAs_InsecureSkipVerify(t *testing.T) {
	tlsResult, err := httpc.NewBuilder().
		SetInsecureSkipVerify(true).
		BuildTLSConfig(context.Background())
	require.NoError(t, err)

	cfg, validErr := tlsResult.Validation()
	require.NoError(t, validErr)
	assert.True(t, cfg.InsecureSkipVerify, "InsecureSkipVerify should be respected in system CAs path")
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
}

func TestBuildTLSConfig_ExcludeSystemCAsWithoutCustomCAs(t *testing.T) {
	tlsResult, err := httpc.NewBuilder().
		SetIncludeSystemCAs(false).
		BuildTLSConfig(context.Background())
	require.NoError(t, err)

	cfg, validErr := tlsResult.Validation()
	require.NoError(t, validErr)
	require.NotNil(t, cfg.RootCAs)
	assert.Empty(t, cfg.RootCAs.Subjects())
}

// TestBuildTLSConfig_CertBytes_SecureDefaults verifies that client cert bytes
// applies secure defaults.
func TestBuildTLSConfig_CertBytes_SecureDefaults(t *testing.T) {
	certPEM, keyPEM := generateTestKeyPair(t)

	tlsResult, err := httpc.NewBuilder().
		SetClientCertBytes(certPEM, keyPEM).
		BuildTLSConfig(context.Background())
	require.NoError(t, err)

	cfg, validErr := tlsResult.Validation()
	require.NoError(t, validErr)
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion, "cert bytes path should enforce TLS 1.2 minimum")
	assert.NotEmpty(t, cfg.CipherSuites, "cert bytes path should set cipher suites")
}

// TestBuildTLSConfig_CertBytes_InsecureSkipVerify verifies that SetInsecureSkipVerify
// is respected when using SetClientCertBytes.
func TestBuildTLSConfig_CertBytes_InsecureSkipVerify(t *testing.T) {
	certPEM, keyPEM := generateTestKeyPair(t)

	tlsResult, err := httpc.NewBuilder().
		SetClientCertBytes(certPEM, keyPEM).
		SetInsecureSkipVerify(true).
		BuildTLSConfig(context.Background())
	require.NoError(t, err)

	cfg, validErr := tlsResult.Validation()
	require.NoError(t, validErr)
	assert.True(t, cfg.InsecureSkipVerify, "InsecureSkipVerify should be respected in cert bytes path")
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
}

func TestBuildTLSConfig_ClientCertBytesOverrideClientCertFiles(t *testing.T) {
	certPEM, keyPEM := generateTestKeyPair(t)
	tmpDir := t.TempDir()

	tlsResult, err := httpc.NewBuilder().
		SetClientCertFiles(filepath.Join(tmpDir, "missing.crt"), filepath.Join(tmpDir, "missing.key")).
		SetClientCertBytes(certPEM, keyPEM).
		BuildTLSConfig(context.Background())
	require.NoError(t, err)

	_, validErr := tlsResult.Validation()
	require.NoError(t, validErr)
}

// TestBuildTLSConfig_FileBased_SecureDefaults verifies that file-based TLS config
// produces configs with secure defaults (via newTLSConfig → tlsconfig.NewClientConfig).
func TestBuildTLSConfig_FileBased_SecureDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	caFile := filepath.Join(tmpDir, "ca.pem")
	writeTestCACertFile(t, caFile)

	tlsResult, err := httpc.NewBuilder().
		AddCACertFiles(caFile).
		BuildTLSConfig(context.Background())
	require.NoError(t, err)

	cfg, validErr := tlsResult.Validation()
	require.NoError(t, validErr)
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion, "file-based path should enforce TLS 1.2 minimum")
	assert.NotEmpty(t, cfg.CipherSuites, "file-based path should set cipher suites")
}

// TestBuildTLSConfig_FileBased_InsecureSkipVerify verifies that file-based config
// respects SetInsecureSkipVerify.
func TestBuildTLSConfig_FileBased_InsecureSkipVerify(t *testing.T) {
	tlsResult, err := httpc.NewBuilder().
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

	tlsResult, err := httpc.NewBuilder().
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

	tlsResult, err := httpc.NewBuilder().
		AddCACertFiles(caFile).
		SetClientCertFiles(certFile, keyFile).
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

func TestBuildTLSConfig_DynamicCertReload(t *testing.T) {
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "client.crt")
	keyFile := filepath.Join(tmpDir, "client.key")

	certPEM1, keyPEM1 := generateTestKeyPair(t)
	require.NoError(t, os.WriteFile(certFile, certPEM1, 0600))
	require.NoError(t, os.WriteFile(keyFile, keyPEM1, 0600))

	tlsResult, err := httpc.NewBuilder().
		SetClientCertFiles(certFile, keyFile).
		SetDynamicCertReload(true).
		BuildTLSConfig(context.Background())
	require.NoError(t, err)

	cfg, validErr := tlsResult.Validation()
	require.NoError(t, validErr)
	require.NotNil(t, cfg.GetClientCertificate)
	assert.Empty(t, cfg.Certificates)

	cert1, err := cfg.GetClientCertificate(nil)
	require.NoError(t, err)
	require.NotEmpty(t, cert1.Certificate)
	original := cert1.Certificate[0]

	certPEM2, keyPEM2 := generateTestKeyPair(t)
	require.NoError(t, os.WriteFile(certFile, certPEM2, 0600))
	require.NoError(t, os.WriteFile(keyFile, keyPEM2, 0600))

	cert2, err := cfg.GetClientCertificate(nil)
	require.NoError(t, err)
	require.NotEmpty(t, cert2.Certificate)
	assert.NotEqual(t, original, cert2.Certificate[0])
}

// TestBuildTLSConfig_NoTLSSettings verifies that with no explicit TLS settings
// (system CAs included by default) still produces a valid config with secure defaults.
func TestBuildTLSConfig_NoTLSSettings(t *testing.T) {
	tlsResult, err := httpc.NewBuilder().
		BuildTLSConfig(context.Background())
	require.NoError(t, err)

	cfg, validErr := tlsResult.Validation()
	require.NoError(t, validErr)
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
}

// TestBuildTLSConfig_Clone_TLSIsolation verifies that cloning the builder
// isolates the escape-hatch TLS config between the original and clone.
func TestBuildTLSConfig_Clone_TLSIsolation(t *testing.T) {
	b1 := httpc.NewBuilder().
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

// TestBuildTLSConfig_SystemCAs_PlusCAFile verifies that when a CA file is added
// alongside the default system CAs, the resulting pool contains both the system
// CAs and the custom CA.
func TestBuildTLSConfig_SystemCAs_PlusCAFile(t *testing.T) {
	tmpDir := t.TempDir()
	caFile := filepath.Join(tmpDir, "custom-ca.pem")
	writeTestCACertFileWithOrg(t, caFile, "Custom Test CA")

	tlsResult, err := httpc.NewBuilder().
		AddCACertFiles(caFile).
		BuildTLSConfig(context.Background())
	require.NoError(t, err)

	cfg, validErr := tlsResult.Validation()
	require.NoError(t, validErr)
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion, "should enforce TLS 1.2 minimum")
	assert.NotNil(t, cfg.RootCAs, "RootCAs should be set")

	// The pool should contain more certs than just the custom CA
	// (system CAs are merged in by default).
	systemPool, err := x509.SystemCertPool()
	require.NoError(t, err)
	systemSubjects := systemPool.Subjects()
	configSubjects := cfg.RootCAs.Subjects()
	assert.Greater(t, len(configSubjects), len(systemSubjects),
		"config pool should have more certs than system pool alone (custom CA added)")
}

// TestBuildTLSConfig_SystemCAs_PlusCertBytes verifies that AddCACertBytes
// works alongside the default system CAs.
func TestBuildTLSConfig_SystemCAs_PlusCertBytes(t *testing.T) {
	tmpDir := t.TempDir()
	caFile := filepath.Join(tmpDir, "ca.pem")
	writeTestCACertFileWithOrg(t, caFile, "Bytes Test CA")
	caPEM, err := os.ReadFile(caFile)
	require.NoError(t, err)

	tlsResult, err := httpc.NewBuilder().
		AddCACertBytes(caPEM).
		BuildTLSConfig(context.Background())
	require.NoError(t, err)

	cfg, validErr := tlsResult.Validation()
	require.NoError(t, validErr)
	assert.NotNil(t, cfg.RootCAs, "RootCAs should be set")

	systemPool, err := x509.SystemCertPool()
	require.NoError(t, err)
	assert.Greater(t, len(cfg.RootCAs.Subjects()), len(systemPool.Subjects()),
		"config pool should include system CAs plus the custom cert")
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
