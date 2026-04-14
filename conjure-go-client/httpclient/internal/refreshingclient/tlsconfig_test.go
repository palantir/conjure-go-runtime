// Copyright (c) 2024 Palantir Technologies. All rights reserved.
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

package refreshingclient

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func generateTestCertAndKey(t *testing.T) (certPEM, keyPEM []byte, certFile, keyFile string) {
	t.Helper()
	dir := t.TempDir()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(time.Hour),
		KeyUsage:  x509.KeyUsageDigitalSignature,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	require.NoError(t, err)

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	require.NoError(t, os.WriteFile(certFile, certPEM, 0600))
	require.NoError(t, os.WriteFile(keyFile, keyPEM, 0600))
	return certPEM, keyPEM, certFile, keyFile
}

func TestNewTLSConfig_DynamicCertReload(t *testing.T) {
	_, _, certFile, keyFile := generateTestCertAndKey(t)

	t.Run("static mode sets Certificates", func(t *testing.T) {
		cfg, err := NewTLSConfig(context.Background(), TLSParams{
			CertFile: certFile,
			KeyFile:  keyFile,
		})
		require.NoError(t, err)
		assert.NotEmpty(t, cfg.Certificates)
		assert.Nil(t, cfg.GetClientCertificate)
	})

	t.Run("dynamic mode sets GetClientCertificate", func(t *testing.T) {
		cfg, err := NewTLSConfig(context.Background(), TLSParams{
			CertFile:          certFile,
			KeyFile:           keyFile,
			DynamicCertReload: true,
		})
		require.NoError(t, err)
		assert.Empty(t, cfg.Certificates)
		assert.NotNil(t, cfg.GetClientCertificate)
	})
}

func TestNewTLSConfig_DynamicReload_PicksUpRotatedCert(t *testing.T) {
	_, _, certFile, keyFile := generateTestCertAndKey(t)

	cfg, err := NewTLSConfig(context.Background(), TLSParams{
		CertFile:          certFile,
		KeyFile:           keyFile,
		DynamicCertReload: true,
	})
	require.NoError(t, err)

	cert1, err := cfg.GetClientCertificate(nil)
	require.NoError(t, err)
	original := cert1.Certificate[0]

	_, _, newCertFile, newKeyFile := generateTestCertAndKey(t)
	newCertPEM, err := os.ReadFile(newCertFile)
	require.NoError(t, err)
	newKeyPEM, err := os.ReadFile(newKeyFile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(certFile, newCertPEM, 0600))
	require.NoError(t, os.WriteFile(keyFile, newKeyPEM, 0600))

	cert2, err := cfg.GetClientCertificate(nil)
	require.NoError(t, err)
	assert.NotEqual(t, original, cert2.Certificate[0])
}

func TestNewTLSConfig_StaticMode_DoesNotPickUpRotatedCert(t *testing.T) {
	_, _, certFile, keyFile := generateTestCertAndKey(t)

	cfg, err := NewTLSConfig(context.Background(), TLSParams{
		CertFile: certFile,
		KeyFile:  keyFile,
	})
	require.NoError(t, err)
	original := cfg.Certificates[0].Certificate[0]

	_, _, newCertFile, newKeyFile := generateTestCertAndKey(t)
	newCertPEM, err := os.ReadFile(newCertFile)
	require.NoError(t, err)
	newKeyPEM, err := os.ReadFile(newKeyFile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(certFile, newCertPEM, 0600))
	require.NoError(t, os.WriteFile(keyFile, newKeyPEM, 0600))

	assert.Equal(t, original, cfg.Certificates[0].Certificate[0])
}
