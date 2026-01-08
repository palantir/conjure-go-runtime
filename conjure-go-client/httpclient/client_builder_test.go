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
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
	tlsCapturingMiddleware := httpclient.MiddlewareFunc(
		func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
			transport := unwrapTransport(next)
			for _, subject := range transport.TLSClientConfig.RootCAs.Subjects() {
				results := strings.Split(strings.TrimSpace(string(subject)), "\a")
				last := results[len(results)-1]
				results = strings.Split(last, "\t")
				last = results[len(results)-1]
				capturedSubjects[last] = struct{}{}
			}
			return next.RoundTrip(req)
		},
	)

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
	assert.ErrorContains(t, err, "test-service")
	assert.Equal(t, capturedSubjects, map[string]struct{}{
		"Test CA": {},
	})

	// Update config to use both CA files
	capturedSubjects = map[string]struct{}{}
	cfg.Security.CAFiles = []string{caFile1, caFile2}
	clientConfigRefreshable.Update(cfg)
	_, err = scopedTokenClient.Delete(context.Background())
	assert.ErrorContains(t, err, "test-service")
	assert.Equal(t, map[string]struct{}{
		"Test CA":   {},
		"Test CA 2": {},
	}, capturedSubjects)
	// Append a file and see the newest CA
	capturedSubjects = map[string]struct{}{}
	_, err = scopedTokenClient.Delete(context.Background())
	assert.ErrorContains(t, err, "test-service")
	assert.Eventually(t, func() bool {
		return reflect.DeepEqual(map[string]struct{}{
			"Test CA":   {},
			"Test CA 2": {},
		}, capturedSubjects)
	}, time.Second*2, time.Millisecond*100)
}

func TestCAUpdatesToTheSameCAFileIsCaptured(t *testing.T) {
	t.Skip("skipping test until feature complete")
	// Create a temp directory with CA certificate files
	tmpDir := t.TempDir()
	caFile1 := filepath.Join(tmpDir, "ca1.pem")
	createTestCACertFile(t, caFile1, 1, "Test CA")

	// Track unique CA subjects captured during requests
	capturedSubjects := make(map[string]struct{})
	tlsCapturingMiddleware := httpclient.MiddlewareFunc(
		func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
			transport := unwrapTransport(next)
			for _, subject := range transport.TLSClientConfig.RootCAs.Subjects() {
				results := strings.Split(strings.TrimSpace(string(subject)), "\a")
				last := results[len(results)-1]
				results = strings.Split(last, "\t")
				last = results[len(results)-1]
				capturedSubjects[last] = struct{}{}
			}
			return next.RoundTrip(req)
		},
	)

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
	scopedTokenClient, err := httpclient.NewClientFromRefreshableConfig(
		context.Background(),
		clientConfigRefreshable,
		httpclient.WithMiddleware(tlsCapturingMiddleware),
	)
	require.NoError(t, err)
	_, err = scopedTokenClient.Delete(context.Background())
	assert.ErrorContains(t, err, "test-service")
	assert.Equal(t, capturedSubjects, map[string]struct{}{
		"Test CA": {},
	})
	// Append a file and see the newest CA
	capturedSubjects = map[string]struct{}{}
	appendTestCACertFile(t, caFile1, 3, "Test CA 3")
	// Ensure the underlying refreshable can sync
	_, err = scopedTokenClient.Delete(context.Background())
	assert.ErrorContains(t, err, "test-service")
	assert.Eventually(t, func() bool {
		return reflect.DeepEqual(map[string]struct{}{
			"Test CA":   {},
			"Test CA 3": {},
		}, capturedSubjects)
	}, time.Second*2, time.Millisecond*100)
}

// unwrapTransport traverses the RoundTripper chain to find the underlying *http.Transport.
func unwrapTransport(rt http.RoundTripper) *http.Transport {
	for rt != nil {
		switch t := rt.(type) {
		case *http.Transport:
			return t
		case interface{ Current() *http.Transport }:
			// RefreshableTransport has a Current() method that returns the underlying transport
			return t.Current()
		default:
			rt = getUnexportedBaseTransport(rt)
		}
	}
	return nil
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
	defer f.Close()
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
