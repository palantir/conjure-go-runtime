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
	"runtime"
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

func TestAddingCAFileIsCaptured(t *testing.T) {
	// Create a temp directory with CA certificate files
	tmpDir := t.TempDir()
	caFile1 := filepath.Join(tmpDir, "ca1.pem")
	caFile2 := filepath.Join(tmpDir, "ca2.pem")
	createTestCACertFile(t, caFile1, 1, "Test CA")
	createTestCACertFile(t, caFile2, 2, "Test CA 2")

	cfg := httpclient.ClientConfig{
		ServiceName:   "baz",
		MaxNumRetries: new(0),
		Security: httpclient.SecurityConfig{
			CAFiles: []string{caFile1},
		},
	}
	clientConfigRefreshable := refreshable.New(cfg)
	httpClient, err := httpclient.NewHTTPClientFromRefreshableConfig(
		context.Background(),
		clientConfigRefreshable,
	)
	require.NoError(t, err)

	// Verify initial CA is loaded
	transport := unwrapTransport(httpClient.Current().Transport)
	require.NotNil(t, transport)
	capturedSubjects := extractCASubjects(transport)
	assert.True(t, containsAllKeys(capturedSubjects, []string{"Test CA"}))

	// Update config to use both CA files
	cfg.Security.CAFiles = []string{caFile1, caFile2}
	clientConfigRefreshable.Update(cfg)
	assert.Eventually(t, func() bool {
		transport := unwrapTransport(httpClient.Current().Transport)
		if transport == nil {
			return false
		}
		subjects := extractCASubjects(transport)
		return containsAllKeys(subjects, []string{"Test CA", "Test CA 2"})
	}, time.Second*2, time.Millisecond*100)
}

// TestCAUpdatesToTheSameCAFileIsCaptured ensures that refreshable and non-refreshable clients still respect on disk CA refreshes
func TestCAUpdatesToTheSameCAFileIsCaptured(t *testing.T) {
	// Create a temp directory with CA certificate files
	tmpDir := t.TempDir()
	caFile1 := filepath.Join(tmpDir, "ca1.pem")
	createTestCACertFile(t, caFile1, 1, "Test CA")

	cfg := httpclient.ClientConfig{
		ServiceName:   "baz",
		MaxNumRetries: new(0),
		Security: httpclient.SecurityConfig{
			CAFiles: []string{caFile1},
		},
	}
	clientConfigRefreshable := refreshable.New(cfg)
	httpClient1, err := httpclient.NewHTTPClientFromRefreshableConfig(
		context.Background(),
		clientConfigRefreshable,
	)
	require.NoError(t, err)

	httpClient2, err := httpclient.NewHTTPClient(
		httpclient.WithConfigForHTTPClient(cfg),
	)
	require.NoError(t, err)

	// Client 1 (refreshable)
	assert.Eventually(t, func() bool {
		transport := unwrapTransport(httpClient1.Current().Transport)
		if transport == nil {
			return false
		}
		subjects := extractCASubjects(transport)
		return containsAllKeys(subjects, []string{"Test CA"})
	}, time.Second*5, time.Millisecond*100)

	// Client 2 (static)
	transport2 := unwrapTransport(httpClient2.Transport)
	require.NotNil(t, transport2)
	subjects2 := extractCASubjects(transport2)
	assert.True(t, containsAllKeys(subjects2, []string{"Test CA"}))

	// Append a file and see the newest CA
	appendTestCACertFile(t, caFile1, 3, "Test CA 3")

	// Client 1
	assert.Eventually(t, func() bool {
		transport := unwrapTransport(httpClient1.Current().Transport)
		if transport == nil {
			return false
		}
		subjects := extractCASubjects(transport)
		return containsAllKeys(subjects, []string{"Test CA", "Test CA 3"})
	}, time.Second*5, time.Millisecond*100)

	// Client 2
	assert.Eventually(t, func() bool {
		transport := unwrapTransport(httpClient2.Transport)
		if transport == nil {
			return false
		}
		subjects := extractCASubjects(transport)
		return containsAllKeys(subjects, []string{"Test CA", "Test CA 3"})
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

	cfg := httpclient.ClientConfig{
		ServiceName: "test-service",
		Security: httpclient.SecurityConfig{
			CAFiles: []string{caFile},
		},
	}
	clientConfigRefreshable := refreshable.New(cfg)
	httpClient1, err := httpclient.NewHTTPClientFromRefreshableConfig(
		context.Background(),
		clientConfigRefreshable,
		httpclient.WithTLSCABytes(caBytesRefreshable),
	)
	require.NoError(t, err)
	httpClient2, err := httpclient.NewHTTPClient(
		httpclient.WithConfigForHTTPClient(cfg),
		httpclient.WithTLSCABytes(caBytesRefreshable),
	)
	require.NoError(t, err)

	// Ensure the initial CA bundle is loaded
	transport1 := unwrapTransport(httpClient1.Current().Transport)
	require.NotNil(t, transport1)
	subjects1 := extractCASubjects(transport1)
	assert.True(t, containsAllKeys(subjects1, []string{"Disk CA", "Dynamic CA"}))

	transport2 := unwrapTransport(httpClient2.Transport)
	require.NotNil(t, transport2)
	subjects2 := extractCASubjects(transport2)
	assert.True(t, containsAllKeys(subjects2, []string{"Disk CA", "Dynamic CA"}))

	// Update dynamic CA bytes and verify it refreshes
	newDynamicCABytes := generateTestCACertPEM(t, 3, "New Dynamic CA")
	caBytesRefreshable.Update([][]byte{newDynamicCABytes})
	// Re-Check
	assert.Eventually(t, func() bool {
		transport := unwrapTransport(httpClient1.Current().Transport)
		if transport == nil {
			return false
		}
		subjects := extractCASubjects(transport)
		return containsAllKeys(subjects, []string{"Disk CA", "New Dynamic CA"})
	}, time.Second*2, time.Millisecond*100)
	assert.Eventually(t, func() bool {
		transport := unwrapTransport(httpClient2.Transport)
		if transport == nil {
			return false
		}
		subjects := extractCASubjects(transport)
		return containsAllKeys(subjects, []string{"Disk CA", "New Dynamic CA"})
	}, time.Second*2, time.Millisecond*100)
}

// extractCASubjects extracts organization names from the root CAs in the transport's TLS config.
func extractCASubjects(transport *http.Transport) map[string]struct{} {
	subjects := make(map[string]struct{})
	if transport.TLSClientConfig == nil || transport.TLSClientConfig.RootCAs == nil {
		return subjects
	}
	for _, subject := range transport.TLSClientConfig.RootCAs.Subjects() {
		var rdnSeq pkix.RDNSequence
		if _, err := asn1.Unmarshal(subject, &rdnSeq); err == nil {
			var name pkix.Name
			name.FillFromRDNSequence(&rdnSeq)
			for _, org := range name.Organization {
				subjects[org] = struct{}{}
			}
		}
	}
	return subjects
}

// unwrapTransport traverses the RoundTripper chain to find the underlying *http.Transport.
func unwrapTransport(rt http.RoundTripper) *http.Transport {
	for rt != nil {
		switch v := rt.(type) {
		case *http.Transport:
			return v
		default:
			if transport := unwrapRefreshableValidatedTransport(rt); transport != nil {
				return transport
			}
			next := getUnexportedField(rt, "base")
			if next == nil {
				next = getUnexportedField(rt, "baseTransport")
			}
			if next == nil {
				return nil
			}
			rt = next
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

// getUnexportedField uses unsafe reflection to access an unexported http.RoundTripper
// field by name from a struct.
func getUnexportedField(rt http.RoundTripper, fieldName string) http.RoundTripper {
	val := reflect.ValueOf(rt)
	if val.Kind() == reflect.Pointer {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return nil
	}
	field := val.FieldByName(fieldName)
	if !field.IsValid() || !field.CanAddr() {
		return nil
	}
	return *(*http.RoundTripper)(unsafe.Pointer(field.UnsafeAddr()))
}

// containsAllKeys checks that all keys in expected are present in actual.
// This is used instead of reflect.DeepEqual because SystemCertPool() includes system CAs
// which vary by OS and are included in Subjects() on Linux but not macOS.
func containsAllKeys(actual map[string]struct{}, expected []string) bool {
	for _, k := range expected {
		if _, ok := actual[k]; !ok {
			return false
		}
	}
	return true
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

// Test_NewClientDoesNotLeakGoroutines verifies that the clients returned by httpclient.NewClient and
// httpclient.NewHTTPClient do not leak goroutines.
func Test_NewClientDoesNotLeakGoroutines(t *testing.T) {
	const numClients = 1000
	mostClients := int(numClients * .9)

	for _, tc := range []struct {
		name   string
		argVal bool
	}{
		{
			name:   "httpclient.NewClient doesn't leak goroutines",
			argVal: false,
		},
		{
			name:   "httpclient.NewHTTPClient doesn't leak goroutines",
			argVal: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			startNumGoroutines := runtime.NumGoroutine()
			t.Log("Goroutines at start:", startNumGoroutines)

			clients := createNewClients(t, numClients, tc.argVal)
			afterCreateClientsNumGoroutines := runtime.NumGoroutine()
			t.Log("Goroutines after createNewClients, before GC:", afterCreateClientsNumGoroutines)
			// add in some slack in case goroutines other than client ones stopped since startNumGoroutines was recorded
			assert.Greater(t, afterCreateClientsNumGoroutines, startNumGoroutines+mostClients)

			// make clients unreferenced, run GC, and briefly sleep.
			// Multiple GC cycles are needed because runtime.AddCleanup
			// callbacks cascade through multi-level derived refreshable
			// chains: each level must be collected before its cleanup
			// unsubscribes from the next level up.
			_ = clients
			clients = nil
			_ = clients

			for range 20 {
				runtime.GC()
				time.Sleep(10 * time.Millisecond)
			}

			afterGCNumGoroutines := runtime.NumGoroutine()
			t.Log("Goroutines after GC:", afterGCNumGoroutines)
			assert.LessOrEqual(t, afterGCNumGoroutines, startNumGoroutines+int(numClients*0.1))
		})
	}
}

func createNewClients(t *testing.T, nClients int, httpClient bool) []any {
	uris := []string{"https://test-service"}

	tmpDir := t.TempDir()
	caFile := filepath.Join(tmpDir, "ca.pem")
	createTestCACertFile(t, caFile, 1, "Test CA")

	var out []any
	for range nClients {
		var (
			client any
			err    error
		)
		if httpClient {
			client, err = httpclient.NewHTTPClient(
				httpclient.WithCAFiles([]string{caFile}),
			)
		} else {
			client, err = httpclient.NewClient(
				httpclient.WithBaseURLs(uris),
				httpclient.WithCAFiles([]string{caFile}),
			)
		}
		require.NoError(t, err)
		out = append(out, client)
	}
	return out
}
