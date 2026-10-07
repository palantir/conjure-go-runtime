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

package httpclient_test

import (
	"crypto/tls"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient"
	"github.com/palantir/pkg/refreshable/v2"
	"github.com/stretchr/testify/require"
)

func TestMergeClientConfigTLSVersions(t *testing.T) {
	defaults := httpclient.SecurityConfig{TLSMinVersion: new(uint16(tls.VersionTLS12)), TLSMaxVersion: new(uint16(tls.VersionTLS13))}
	for _, test := range []struct {
		name     string
		config   httpclient.SecurityConfig
		expected httpclient.SecurityConfig
	}{
		{"inherit", httpclient.SecurityConfig{}, defaults},
		{"override maximum", httpclient.SecurityConfig{TLSMaxVersion: new(uint16(tls.VersionTLS12))}, httpclient.SecurityConfig{TLSMinVersion: defaults.TLSMinVersion, TLSMaxVersion: new(uint16(tls.VersionTLS12))}},
		{"explicit zero", httpclient.SecurityConfig{TLSMinVersion: new(uint16(0)), TLSMaxVersion: new(uint16(0))}, httpclient.SecurityConfig{TLSMinVersion: new(uint16(0)), TLSMaxVersion: new(uint16(0))}},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := httpclient.MergeClientConfig(httpclient.ClientConfig{Security: test.config}, httpclient.ClientConfig{Security: defaults})
			require.Equal(t, test.expected, config.Security)
		})
	}
}

func TestHTTPClientTLSVersionConfig(t *testing.T) {
	for _, test := range []struct {
		name        string
		security    httpclient.SecurityConfig
		expectedMin uint16
		expectedMax uint16
	}{
		{"defaults", httpclient.SecurityConfig{}, tls.VersionTLS12, 0},
		{"TLS 1.3", httpclient.SecurityConfig{TLSMinVersion: new(uint16(tls.VersionTLS13)), TLSMaxVersion: new(uint16(tls.VersionTLS13))}, tls.VersionTLS13, tls.VersionTLS13},
		{"explicit zero", httpclient.SecurityConfig{TLSMinVersion: new(uint16(0)), TLSMaxVersion: new(uint16(0))}, 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := httpclient.ClientConfig{ServiceName: "test", Security: test.security}
			client, err := httpclient.NewHTTPClientWithContext(t.Context(), httpclient.WithConfigForHTTPClient(config))
			require.NoError(t, err)
			clients, err := httpclient.NewHTTPClientFromRefreshableConfig(t.Context(), refreshable.New(config))
			require.NoError(t, err)
			for name, client := range map[string]*http.Client{"static": client, "refreshable": clients.Current()} {
				t.Run(name, func(t *testing.T) {
					config := unwrapTransport(client.Transport).TLSClientConfig
					require.Equal(t, test.expectedMin, config.MinVersion)
					require.Equal(t, test.expectedMax, config.MaxVersion)
				})
			}
		})
	}
}

func TestClientTLSVersionConfigRefresh(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server.Config.SetKeepAlivesEnabled(false)
	t.Cleanup(server.Close)
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600))
	config := httpclient.ClientConfig{
		ServiceName: "test", URIs: []string{server.URL}, MaxNumRetries: new(0),
		Security: httpclient.SecurityConfig{CAFiles: []string{caFile}, TLSMaxVersion: new(uint16(tls.VersionTLS12))},
	}
	staticClient, err := httpclient.NewClientWithContext(t.Context(), httpclient.WithConfig(config))
	require.NoError(t, err)
	resp, err := staticClient.Get(t.Context())
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, uint16(tls.VersionTLS12), resp.TLS.Version)

	configs := refreshable.New(config)
	client, err := httpclient.NewClientFromRefreshableConfig(t.Context(), configs)
	require.NoError(t, err)
	for _, test := range []struct {
		name            string
		minVersion      *uint16
		maxVersion      *uint16
		expectedVersion uint16
	}{
		{"initial TLS 1.2", nil, new(uint16(tls.VersionTLS12)), tls.VersionTLS12},
		{"TLS 1.3", new(uint16(tls.VersionTLS13)), new(uint16(tls.VersionTLS13)), tls.VersionTLS13},
		{"explicit zero", new(uint16(0)), new(uint16(0)), tls.VersionTLS13},
		{"unset", nil, nil, tls.VersionTLS13},
	} {
		t.Run(test.name, func(t *testing.T) {
			config.Security.TLSMinVersion = test.minVersion
			config.Security.TLSMaxVersion = test.maxVersion
			configs.Update(config)
			resp, err := client.Get(t.Context())
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Equal(t, test.expectedVersion, resp.TLS.Version)
		})
	}
}
