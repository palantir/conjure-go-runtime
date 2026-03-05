// Copyright (c) 2018 Palantir Technologies. All rights reserved.
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

package httpclient_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient"
	"github.com/palantir/pkg/refreshable/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrapTransport(t *testing.T) {
	expected := []int{3, 2, 1, 1, 2, 3}
	var tracker []int
	middleware := func(id int) httpclient.MiddlewareFunc {
		return func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
			tracker = append(tracker, id)
			resp, err := next.RoundTrip(req)
			tracker = append(tracker, id)
			return resp, err
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := httpclient.NewClient(
		httpclient.WithMiddleware(middleware(1)),
		httpclient.WithMiddleware(middleware(2)),
		httpclient.WithMiddleware(middleware(3)),
		httpclient.WithBaseURLs([]string{server.URL}),
	)
	require.NoError(t, err)

	resp, err := client.Do(context.Background(), httpclient.WithRequestMethod("GET"))
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, expected, tracker)
}

func TestNewHTTPClientWithoutURIs(t *testing.T) {
	cfg := httpclient.ClientConfig{ServiceName: "test-service"}
	c, err := httpclient.NewHTTPClientFromRefreshableConfig(context.Background(), refreshable.New(cfg))
	require.NoError(t, err)
	require.NotNil(t, c.Current())
}

func toPointer[T any](timeArg T) *T {
	return &timeArg
}

func TestAddingCAFileIsCaptured(t *testing.T) {
	// Create a temp directory with CA certificate files
	tmpDir := t.TempDir()
	caFile1 := filepath.Join(tmpDir, "ca1.pem")
	caFile2 := filepath.Join(tmpDir, "ca2.pem")
	createTestCACertFile(t, caFile1, 1, "Test CA")
	createTestCACertFile(t, caFile2, 2, "Test CA 2")
	// Track unique CA subjects captured during requests
	capturedSubjects := make(map[string]struct{})
	tlsCapturingMiddleware := newTLSCapturingMiddleware(&capturedSubjects)
	cfg := httpclient.ClientConfig{
		ServiceName:   "baz",
		MaxNumRetries: toPointer(0),
		URIs: []string{
			"https://test-service",
		},
		Security: httpclient.SecurityConfig{
			CAFiles: []string{caFile1},
		},
	}
	clientConfigRefreshable := refreshable.New(cfg)
	scopedTokenClient, err := httpclient.NewClientFromRefreshableConfig(
		context.Background(),
		clientConfigRefreshable,
		httpclient.WithMiddleware(tlsCapturingMiddleware),
	)
	require.NoError(t, err)
	_, err = scopedTokenClient.Delete(context.Background())
	assert.Error(t, err)
	assert.Eventually(t, func() bool {
		return reflect.DeepEqual(map[string]struct{}{
			"Test CA": {},
		}, capturedSubjects)
	}, time.Second*2, time.Millisecond*100)
	// Update config to use both CA files
	capturedSubjects = map[string]struct{}{}
	cfg.Security.CAFiles = []string{caFile1, caFile2}
	clientConfigRefreshable.Update(cfg)
	_, err = scopedTokenClient.Delete(context.Background())
	assert.Error(t, err)
	assert.Eventually(t, func() bool {
		return reflect.DeepEqual(map[string]struct{}{
			"Test CA":   {},
			"Test CA 2": {},
		}, capturedSubjects)
	}, time.Second*2, time.Millisecond*100)
}

// TestCAUpdatesToTheSameCAFileIsCaptured ensures that refreshable and non-refreshable clients still respect on disk CA refreshes
func TestCAUpdatesToTheSameCAFileIsCaptured(t *testing.T) {
	// Create a temp directory with CA certificate files
	tmpDir := t.TempDir()
	caFile1 := filepath.Join(tmpDir, "ca1.pem")
	createTestCACertFile(t, caFile1, 1, "Test CA")

	// Track unique CA subjects captured during requests
	capturedSubjects1 := make(map[string]struct{})
	tlsCapturingMiddleware1 := newTLSCapturingMiddleware(&capturedSubjects1)
	capturedSubjects2 := make(map[string]struct{})
	tlsCapturingMiddleware2 := newTLSCapturingMiddleware(&capturedSubjects2)
	cfg := httpclient.ClientConfig{
		ServiceName: "baz",
		URIs: []string{
			"https://test-service",
		},
		MaxNumRetries: toPointer(0),
		Security: httpclient.SecurityConfig{
			CAFiles: []string{caFile1},
		},
	}
	clientConfigRefreshable := refreshable.New(cfg)
	client1, err := httpclient.NewClientFromRefreshableConfig(
		context.Background(),
		clientConfigRefreshable,
		httpclient.WithMiddleware(tlsCapturingMiddleware1),
	)
	require.NoError(t, err)
	client2, err := httpclient.NewClient(
		httpclient.WithConfig(cfg),
		httpclient.WithMiddleware(tlsCapturingMiddleware2),
	)
	require.NoError(t, err)
	// Client 1
	assert.Eventually(t, func() bool {
		capturedSubjects1 = map[string]struct{}{}
		_, err = client1.Delete(context.Background())
		assert.Error(t, err)
		return reflect.DeepEqual(map[string]struct{}{
			"Test CA": {},
		}, capturedSubjects1)
	}, time.Second*5, time.Millisecond*100)
	// Client 2
	assert.Eventually(t, func() bool {
		capturedSubjects2 = map[string]struct{}{}
		_, err = client2.Delete(context.Background())
		assert.Error(t, err)
		return reflect.DeepEqual(map[string]struct{}{
			"Test CA": {},
		}, capturedSubjects2)
	}, time.Second*5, time.Millisecond*100)
	// Append a file and see the newest CA

	appendTestCACertFile(t, caFile1, 3, "Test CA 3")
	// Client 1
	assert.Eventually(t, func() bool {
		capturedSubjects1 = map[string]struct{}{}
		_, err = client1.Delete(context.Background())
		assert.Error(t, err)
		return reflect.DeepEqual(map[string]struct{}{
			"Test CA":   {},
			"Test CA 3": {},
		}, capturedSubjects1)
	}, time.Second*5, time.Millisecond*100)
	// Client 2
	assert.Eventually(t, func() bool {
		capturedSubjects2 = map[string]struct{}{}
		_, err = client2.Delete(context.Background())
		assert.Error(t, err)
		return reflect.DeepEqual(map[string]struct{}{
			"Test CA":   {},
			"Test CA 3": {},
		}, capturedSubjects2)
	}, time.Second*5, time.Millisecond*100)
}

// TestMissingCAFilesCausesError ensures that we can't create clients that start broken
func TestMissingCAFilesCausesError(t *testing.T) {
	// Create a temp directory with CA certificate files
	cfg := httpclient.ClientConfig{
		URIs: []string{
			"https://test-service",
		},
		Security: httpclient.SecurityConfig{
			CAFiles: []string{filepath.Join("fakedir/", "ca1.pem")},
		},
	}
	clientConfigRefreshable := refreshable.New(cfg)
	_, err := httpclient.NewClientFromRefreshableConfig(
		context.Background(),
		clientConfigRefreshable,
	)
	assert.ErrorContains(t, err, "open fakedir/ca1.pem: no such file or directory")
	_, err = httpclient.NewClient(
		httpclient.WithConfig(cfg),
	)
	assert.ErrorContains(t, err, "open fakedir/ca1.pem: no such file or directory")
}

func TestMissingCertAndKeyFileErrors(t *testing.T) {
	// Create a temp directory with CA certificate files
	cfg := httpclient.ClientConfig{
		URIs: []string{
			"https://test-service",
		},
		Security: httpclient.SecurityConfig{
			KeyFile:  filepath.Join("fakedir/", "key"),
			CertFile: filepath.Join("fakedir/", "cert"),
		},
	}
	clientConfigRefreshable := refreshable.New(cfg)
	_, err := httpclient.NewClientFromRefreshableConfig(
		context.Background(),
		clientConfigRefreshable,
	)
	assert.ErrorContains(t, err, "open fakedir/cert: no such file or directory")
	_, err = httpclient.NewClient(
		httpclient.WithConfig(cfg),
	)
	assert.ErrorContains(t, err, "open fakedir/cert: no such file or directory")
}

func TestJustMissingCertDoesntError(t *testing.T) {
	// Create a temp directory with CA certificate files
	cfg := httpclient.ClientConfig{
		URIs: []string{
			"https://test-service",
		},
		Security: httpclient.SecurityConfig{
			CertFile: filepath.Join("fakedir/", "cert"),
		},
	}
	clientConfigRefreshable := refreshable.New(cfg)
	_, err := httpclient.NewClientFromRefreshableConfig(
		context.Background(),
		clientConfigRefreshable,
	)
	assert.NoError(t, err)
	_, err = httpclient.NewClient(
		httpclient.WithConfig(cfg),
	)
	assert.NoError(t, err)
}

func TestJustMissingKeyDoesntError(t *testing.T) {
	// Create a temp directory with CA certificate files
	cfg := httpclient.ClientConfig{
		URIs: []string{
			"https://test-service",
		},
		Security: httpclient.SecurityConfig{
			KeyFile: filepath.Join("fakedir/", "key"),
		},
	}
	clientConfigRefreshable := refreshable.New(cfg)
	_, err := httpclient.NewClientFromRefreshableConfig(
		context.Background(),
		clientConfigRefreshable,
	)
	assert.NoError(t, err)
	_, err = httpclient.NewClient(
		httpclient.WithConfig(cfg),
	)
	assert.NoError(t, err)
}

func TestCABytesAndCAFileCombined(t *testing.T) {
	// Create a CA file on disk
	tmpDir := t.TempDir()
	caFile := filepath.Join(tmpDir, "ca.pem")
	createTestCACertFile(t, caFile, 1, "Disk CA")
	// Create CA bytes for the dynamic provider
	dynamicCABytes := generateTestCACertPEM(t, 2, "Dynamic CA")
	caBytesRefreshable := refreshable.New([][]byte{dynamicCABytes})
	// Track unique CA subjects captured during requests
	capturedSubjects1 := make(map[string]struct{})
	tlsCapturingMiddleware1 := newTLSCapturingMiddleware(&capturedSubjects1)
	capturedSubjects2 := make(map[string]struct{})
	tlsCapturingMiddleware2 := newTLSCapturingMiddleware(&capturedSubjects2)
	cfg := httpclient.ClientConfig{
		ServiceName:   "test-service",
		MaxNumRetries: toPointer(0),
		URIs:          []string{"https://test-service"},
		Security: httpclient.SecurityConfig{
			CAFiles: []string{caFile},
		},
	}
	clientConfigRefreshable := refreshable.New(cfg)
	client1, err := httpclient.NewClientFromRefreshableConfig(
		context.Background(),
		clientConfigRefreshable,
		httpclient.WithMiddleware(tlsCapturingMiddleware1),
		httpclient.WithTLSCABytes(caBytesRefreshable),
	)
	require.NoError(t, err)
	client2, err := httpclient.NewClient(
		httpclient.WithConfig(cfg),
		httpclient.WithMiddleware(tlsCapturingMiddleware2),
		httpclient.WithTLSCABytes(caBytesRefreshable),
	)
	require.NoError(t, err)
	// Ensure the initial CA bundle is loaded
	_, err = client1.Delete(context.Background())
	assert.Error(t, err)
	assert.Equal(t, map[string]struct{}{
		"Disk CA":    {},
		"Dynamic CA": {},
	}, capturedSubjects1)
	_, err = client2.Delete(context.Background())
	assert.Error(t, err)
	assert.Equal(t, map[string]struct{}{
		"Disk CA":    {},
		"Dynamic CA": {},
	}, capturedSubjects2)

	// Update dynamic CA bytes and verify it refreshes
	capturedSubjects1 = make(map[string]struct{})
	capturedSubjects2 = make(map[string]struct{})
	newDynamicCABytes := generateTestCACertPEM(t, 3, "New Dynamic CA")
	caBytesRefreshable.Update([][]byte{newDynamicCABytes})
	// Re-Check
	assert.Eventually(t, func() bool {
		capturedSubjects1 = make(map[string]struct{})
		_, err = client1.Delete(context.Background())
		assert.Error(t, err)
		return reflect.DeepEqual(map[string]struct{}{
			"Disk CA":        {},
			"New Dynamic CA": {},
		}, capturedSubjects1)
	}, time.Second*2, time.Millisecond*100)
	assert.Eventually(t, func() bool {
		capturedSubjects2 = make(map[string]struct{})
		_, err = client2.Delete(context.Background())
		assert.Error(t, err)
		return reflect.DeepEqual(map[string]struct{}{
			"Disk CA":        {},
			"New Dynamic CA": {},
		}, capturedSubjects2)
	}, time.Second*2, time.Millisecond*100)
}

func newTLSCapturingMiddleware(capturedSubjects *map[string]struct{}) httpclient.MiddlewareFunc {
	return func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		transport := unwrapTransport(next)
		for _, subject := range transport.TLSClientConfig.RootCAs.Subjects() {
			var rdnSeq pkix.RDNSequence
			if _, err := asn1.Unmarshal(subject, &rdnSeq); err == nil {
				var name pkix.Name
				name.FillFromRDNSequence(&rdnSeq)
				for _, org := range name.Organization {
					(*capturedSubjects)[org] = struct{}{}
				}
			}
		}
		return next.RoundTrip(req)
	}
}

// unwrapTransport traverses the RoundTripper chain to find the underlying *http.Transport.
func unwrapTransport(rt http.RoundTripper) *http.Transport {
	for rt != nil {
		switch rt.(type) {
		case *http.Transport:
			return rt.(*http.Transport)
		default:
			if transport := unwrapRefreshableValidatedTransport(rt); transport != nil {
				return transport
			}
			rt = getUnexportedBaseTransport(rt)
		}
	}
	return nil
}

func unwrapRefreshableValidatedTransport(rt http.RoundTripper) *http.Transport {
	val := reflect.ValueOf(rt)
	if val.Kind() == reflect.Pointer {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return nil
	}
	field := val.FieldByName("Refreshable")
	if !field.IsValid() {
		return nil
	}
	method := field.MethodByName("Unvalidated")
	if !method.IsValid() {
		return nil
	}
	result := method.Call(nil)
	if len(result) == 0 {
		return nil
	}
	transport, ok := result[0].Interface().(*http.Transport)
	if !ok {
		return nil
	}
	return transport
}

// getUnexportedBaseTransport uses unsafe reflection to access the unexported baseTransport
// field from httpclient.wrappedClient middleware wrappers.
func getUnexportedBaseTransport(rt http.RoundTripper) http.RoundTripper {
	val := reflect.ValueOf(rt)
	if val.Kind() == reflect.Pointer {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return nil
	}
	field := val.FieldByName("baseTransport")
	if !field.IsValid() || !field.CanAddr() {
		return nil
	}
	return *(*http.RoundTripper)(unsafe.Pointer(field.UnsafeAddr()))
}

func createTestCACertFile(t *testing.T, filePath string, serialNumber int64, orgName string) {
	certPEM := generateTestCACertPEM(t, serialNumber, orgName)
	err := os.WriteFile(filePath, certPEM, 0600)
	require.NoError(t, err)
}

func appendTestCACertFile(t *testing.T, filePath string, serialNumber int64, orgName string) {
	certPEM := generateTestCACertPEM(t, serialNumber, orgName)
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_WRONLY, 0600)
	require.NoError(t, err)
	defer func() { assert.NoError(t, f.Close()) }()
	_, err = f.Write(certPEM)
	require.NoError(t, err)
}

func generateTestCACertPEM(t *testing.T, serialNumber int64, orgName string) []byte {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := x509.Certificate{
		SerialNumber: big.NewInt(serialNumber),
		Subject: pkix.Name{
			Organization: []string{orgName},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
}
