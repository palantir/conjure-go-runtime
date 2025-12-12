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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"

	refreshablev1 "github.com/palantir/pkg/refreshable"
	"github.com/palantir/pkg/refreshable/v2"

	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient"
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

func TestDoOurClientsWork(t *testing.T) {
	// Create a temp directory with a CA certificate file

	cfg := httpclient.ClientConfig{
		ServiceName: "baz",
		URIs: []string{
			"https://test-service",
		},
	}
	rrr := refreshable.New(cfg)
	scopedTokenClient, err := httpclient.NewClientFromRefreshableConfig(
		context.Background(),
		httpclient.NewRefreshingClientConfig(refreshablev1.FromV2(rrr)),
	)
	assert.NoError(t, err)
	_, err = scopedTokenClient.Delete(context.Background())
	assert.ErrorContains(t, err, "test-service")
	cfg.URIs = []string{
		"https://foo-service",
	}
	rrr.Update(cfg)
	_, err = scopedTokenClient.Delete(context.Background())
	assert.ErrorContains(t, err, "foo-service")
}

func generateTestCACert(t *testing.T) []byte {
	t.Helper()

	// Generate a private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	// Create a self-signed CA certificate
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test CA"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	require.NoError(t, err)

	// Encode to PEM
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	return certPEM
}
