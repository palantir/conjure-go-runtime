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
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient"
	"github.com/palantir/pkg/refreshable/v2"
	"github.com/stretchr/testify/require"
)

func TestDisableSystemCertPool(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	caPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: server.Certificate().Raw,
	})
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(caFile, caPEM, 0600))

	for _, withCA := range []bool{false, true} {
		config := httpclient.ClientConfig{
			ServiceName: "test",
			Security: httpclient.SecurityConfig{
				DisableSystemCertPool: new(true),
			},
		}
		params := []httpclient.HTTPClientParam{httpclient.WithTLSDisableSystemCertPool()}
		expectedPool := x509.NewCertPool()
		if withCA {
			config.Security.CAFiles = []string{caFile}
			params = append(params, httpclient.WithTLSCABytes(refreshable.New([][]byte{caPEM})))
			require.True(t, expectedPool.AppendCertsFromPEM(caPEM))
		}
		fromParam, err := httpclient.NewHTTPClientWithContext(t.Context(), params...)
		require.NoError(t, err)
		fromConfig, err := httpclient.NewHTTPClientWithContext(t.Context(), httpclient.WithConfigForHTTPClient(config))
		require.NoError(t, err)
		configs := refreshable.New(config)
		fromRefreshable, err := httpclient.NewHTTPClientFromRefreshableConfig(t.Context(), configs)
		require.NoError(t, err)
		for name, client := range map[string]*http.Client{
			"parameter":   fromParam,
			"config":      fromConfig,
			"refreshable": fromRefreshable.Current(),
		} {
			t.Run(name+"/withCA="+fmt.Sprint(withCA), func(t *testing.T) {
				pool := unwrapTransport(client.Transport).TLSClientConfig.RootCAs
				require.NotNil(t, pool)
				require.True(t, pool.Equal(expectedPool))
				resp, err := client.Get(server.URL)
				if !withCA {
					var unknownAuthority x509.UnknownAuthorityError
					require.ErrorAs(t, err, &unknownAuthority)
					return
				}
				require.NoError(t, err)
				require.NoError(t, resp.Body.Close())
			})
		}
		if !withCA {
			config.Security.DisableSystemCertPool = new(false)
			configs.Update(config)
			require.Nil(t, unwrapTransport(fromRefreshable.Current().Transport).TLSClientConfig.RootCAs)
			config.Security.DisableSystemCertPool = new(true)
			configs.Update(config)
			pool := unwrapTransport(fromRefreshable.Current().Transport).TLSClientConfig.RootCAs
			require.NotNil(t, pool)
			require.True(t, pool.Equal(expectedPool))
		}
	}
}
