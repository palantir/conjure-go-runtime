// Copyright (c) 2021 Palantir Technologies. All rights reserved.
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

package httpclient

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/palantir/pkg/refreshable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

func TestRefreshableClientConfig(t *testing.T) {
	const serviceName = "serviceName"
	// testDefaultClient is pulled out because we also use it to test the non-refreshable version which is backed by the same infra.
	testDefaultClient := func(t *testing.T, client *clientImpl) {
		httpClient := client.client.CurrentHTTPClient()
		assert.Equal(t, defaultHTTPTimeout, httpClient.Timeout, "http timeout not set to default")

		if client.maxAttempts != nil {
			assert.Nil(t, client.maxAttempts.CurrentIntPtr())
		}
		assert.Equal(t, defaultInitialBackoff, client.backoffOptions.InitialBackoff().CurrentDuration())
		assert.Equal(t, defaultMaxBackoff, client.backoffOptions.MaxBackoff().CurrentDuration())

		initialTransport, initialMiddlewares := unwrapTransport(httpClient.Transport)
		assert.Equal(t, defaultMaxIdleConns, initialTransport.MaxIdleConns)
		assert.Equal(t, defaultMaxIdleConnsPerHost, initialTransport.MaxIdleConnsPerHost)
		assert.Equal(t, defaultIdleConnTimeout, initialTransport.IdleConnTimeout)
		assert.Equal(t, defaultExpectContinueTimeout, initialTransport.ExpectContinueTimeout)
		assert.Equal(t, defaultTLSHandshakeTimeout, initialTransport.TLSHandshakeTimeout)
		assert.Equal(t, false, initialTransport.DisableKeepAlives)
		assert.NotNil(t, initialTransport.Proxy)

		if assert.Len(t, initialMiddlewares, 3) {
			assert.IsType(t, recoveryMiddleware{}, initialMiddlewares[0])
			if assert.IsType(t, traceMiddleware{}, initialMiddlewares[1]) {
				traceM := initialMiddlewares[1].(traceMiddleware)
				assert.False(t, traceM.DisableRequestSpan)
				assert.False(t, traceM.DisableTraceHeaders)
			}
			if assert.IsType(t, &metricsMiddleware{}, initialMiddlewares[2]) {
				metricsM := initialMiddlewares[2].(*metricsMiddleware)
				assert.False(t, metricsM.Disabled.CurrentBool())
				assert.Equal(t, serviceName, metricsM.ServiceName.CurrentString())
			}
		}

		if tlsConfig := initialTransport.TLSClientConfig; assert.NotNil(t, tlsConfig) {
			assert.False(t, tlsConfig.InsecureSkipVerify)
			assert.True(t, tlsConfig.PreferServerCipherSuites)
			if assert.NotZero(t, tlsConfig.MinVersion, "tls min version not set") {
				assert.Equal(t, uint16(tls.VersionTLS12), tlsConfig.MinVersion, "unexpected tls min version")
			}
		}

	}
	t.Run("default static config", func(t *testing.T) {
		client, err := NewClient(WithConfig(ClientConfig{ServiceName: serviceName, URIs: []string{"https://localhost"}}))
		require.NoError(t, err)
		testDefaultClient(t, client.(*clientImpl))
	})

	// Build a refreshable client from the ground up -- start with an empty configuration, then add/mutate values.

	initialConfig := ServicesConfig{
		Default:  ClientConfig{},
		Services: map[string]ClientConfig{},
	}
	initialConfigBytes, err := yaml.Marshal(initialConfig)
	require.NoError(t, err)
	refreshableConfigBytes := refreshable.NewDefaultRefreshable(initialConfigBytes)
	updateRefreshableBytes := func(s ServicesConfig) {
		b, err := yaml.Marshal(s)
		if err != nil {
			panic(err)
		}
		if err := refreshableConfigBytes.Update(b); err != nil {
			panic(err)
		}
	}
	refreshableServicesConfig := NewRefreshingServicesConfig(refreshableConfigBytes.Map(func(i interface{}) interface{} {
		var c ServicesConfig
		if err := yaml.Unmarshal(i.([]byte), &c); err != nil {
			panic(err)
		}
		return c
	}))

	t.Run("refreshable config without uris fails", func(t *testing.T) {
		getClientURIs := func(client Client) []string {
			return client.(*clientImpl).uriScorer.CurrentURIScoringMiddleware().GetURIsInOrderOfIncreasingScore()
		}
		refreshableClientConfig := RefreshableClientConfigFromServiceConfig(refreshableServicesConfig, serviceName)
		client, err := NewClientFromRefreshableConfig(context.Background(), refreshableClientConfig)
		require.EqualError(t, err, "httpclient URLs must not be empty")
		require.Nil(t, client)

		client, err = NewClientFromRefreshableConfig(context.Background(), refreshableClientConfig, WithBaseURL("https://localhost"))
		require.NoError(t, err, "expected to successfully create client using WithBaseURL even when config has no URIs")
		require.Equal(t, []string{"https://localhost"}, getClientURIs(client), "expected URIs to be set")

		client, err = NewClientFromRefreshableConfig(context.Background(), refreshableClientConfig, WithRefreshableBaseURLs(refreshable.NewStringSlice(refreshable.NewDefaultRefreshable([]string{"https://localhost"}))))
		require.NoError(t, err, "expected to successfully create client using WithRefreshableBaseURLs even when config has no URIs")
		require.Equal(t, []string{"https://localhost"}, getClientURIs(client), "expected URIs to be set")

		t.Run("WithAllowCreateWithEmptyURIs", func(t *testing.T) {
			client, err := NewClientFromRefreshableConfig(context.Background(), refreshableClientConfig, WithAllowCreateWithEmptyURIs())
			require.NoError(t, err, "expected to create a client from empty client config with WithAllowCreateWithEmptyURIs")

			// Expect error making request
			_, err = client.Get(context.Background())
			require.EqualError(t, err, ErrEmptyURIs.Error())
			// Update config
			initialConfig.Services[serviceName] = ClientConfig{ServiceName: serviceName, URIs: []string{"https://localhost"}}
			updateRefreshableBytes(initialConfig)

			require.Equal(t, []string{"https://localhost"}, getClientURIs(client), "expected URIs to be set")
		})
	})

	refreshableClientConfig := RefreshableClientConfigFromServiceConfig(refreshableServicesConfig, serviceName)
	initialConfig.Services[serviceName] = ClientConfig{ServiceName: serviceName, URIs: []string{"https://localhost"}}
	updateRefreshableBytes(initialConfig)
	client, err := NewClientFromRefreshableConfig(context.Background(), refreshableClientConfig)
	require.NoError(t, err, "expected to create a client from empty client config")

	t.Run("default refreshable config", func(t *testing.T) {
		testDefaultClient(t, client.(*clientImpl))
	})

	currentHTTPClient := func() *http.Client {
		return client.(*clientImpl).client.CurrentHTTPClient()
	}
	t.Run("update timeout, transport unchanged", func(t *testing.T) {
		oldClient := currentHTTPClient()
		oldTransport, oldMiddlewares := unwrapTransport(oldClient.Transport)

		t.Run("service config", func(t *testing.T) {
			serviceCfg := initialConfig.Services[serviceName]
			serviceCfg.WriteTimeout = newDurationPtr(time.Second)
			initialConfig.Services[serviceName] = serviceCfg
			updateRefreshableBytes(initialConfig)

			newClient := currentHTTPClient()
			newTransport, newMiddlewares := unwrapTransport(newClient.Transport)
			assert.Equal(t, time.Second, newClient.Timeout)
			assert.NotEqual(t, oldClient, newClient, "expected new http client to be constructed")
			assert.Equal(t, oldTransport, newTransport, "expected transport to remain unchanged")
			assert.Equal(t, oldMiddlewares, newMiddlewares, "expected middlewares to remain unchanged")
		})
		t.Run("default config", func(t *testing.T) {
			initialConfig.Default.ReadTimeout = newDurationPtr(time.Hour)
			updateRefreshableBytes(initialConfig)

			newClient := currentHTTPClient()
			newTransport, newMiddlewares := unwrapTransport(newClient.Transport)
			assert.Equal(t, time.Hour, currentHTTPClient().Timeout)
			assert.NotEqual(t, oldClient, newClient, "expected new http client to be constructed")
			assert.Equal(t, oldTransport, newTransport, "expected transport to remain unchanged")
			assert.Equal(t, oldMiddlewares, newMiddlewares, "expected middlewares to remain unchanged")
		})
		t.Run("unset, falls back to default", func(t *testing.T) {
			initialConfig.Default.ReadTimeout = nil
			serviceCfg := initialConfig.Services[serviceName]
			serviceCfg.WriteTimeout = nil
			initialConfig.Services[serviceName] = serviceCfg
			updateRefreshableBytes(initialConfig)

			newClient := currentHTTPClient()
			newTransport, newMiddlewares := unwrapTransport(newClient.Transport)
			assert.Equal(t, defaultHTTPTimeout, newClient.Timeout)
			assert.Equal(t, oldClient, newClient, "expected new http client to be equal to original")
			assert.Equal(t, oldTransport, newTransport, "expected transport to remain unchanged")
			assert.Equal(t, oldMiddlewares, newMiddlewares, "expected middlewares to remain unchanged")
		})
	})

	t.Run("update dial timeout, transport and client unchanged", func(t *testing.T) {
		oldClient := currentHTTPClient()
		oldTransport, oldMiddlewares := unwrapTransport(oldClient.Transport)

		connTimeout := time.Nanosecond
		initialConfig.Default.ConnectTimeout = &connTimeout
		updateRefreshableBytes(initialConfig)

		newClient := currentHTTPClient()
		newTransport, newMiddlewares := unwrapTransport(newClient.Transport)

		assert.Equal(t, oldClient, newClient, "expected http client to remain unchanged")
		assert.Equal(t, oldTransport, newTransport, "expected transport to remain unchanged")
		assert.Equal(t, oldMiddlewares, newMiddlewares, "expected middlewares to remain unchanged")

		// Test that the we time out quickly due to the new value.
		// Unfortunately because transport.DialContext is a function type, we can not inspect the underlying struct.
		start := time.Now()
		conn, dialErr := newTransport.DialContext(context.Background(), "tcp", "palantir.com:443")
		if assert.Error(t, dialErr) {
			assert.Less(t, time.Since(start), time.Second, "Dial should fail immediately due to the 1ns timeout")
			if assert.IsType(t, &net.OpError{}, dialErr) {
				assert.True(t, dialErr.(*net.OpError).Timeout())
			}
		} else {
			_ = conn.Close()
		}
	})

	t.Run("disable http2, transport updates", func(t *testing.T) {
		oldClient := currentHTTPClient()
		oldTransport, _ := unwrapTransport(oldClient.Transport)

		assert.Contains(t, oldTransport.TLSNextProto, "h2")

		disableHTTP2 := true
		initialConfig.Default.DisableHTTP2 = &disableHTTP2
		updateRefreshableBytes(initialConfig)

		newClient := currentHTTPClient()
		newTransport, _ := unwrapTransport(newClient.Transport)

		assert.NotContains(t, newTransport.TLSNextProto, "h2")

		initialConfig.Default.DisableHTTP2 = nil
		updateRefreshableBytes(initialConfig)
	})

	t.Run("disable proxy, transport updates", func(t *testing.T) {
		oldClient := currentHTTPClient()
		oldTransport, _ := unwrapTransport(oldClient.Transport)

		assert.Contains(t, oldTransport.TLSNextProto, "h2")

		disableProxy := false
		initialConfig.Default.ProxyFromEnvironment = &disableProxy
		updateRefreshableBytes(initialConfig)

		newClient := currentHTTPClient()
		newTransport, _ := unwrapTransport(newClient.Transport)

		assert.Nil(t, newTransport.Proxy)

		initialConfig.Default.ProxyFromEnvironment = nil
		updateRefreshableBytes(initialConfig)
	})
}

func newDurationPtr(dur time.Duration) *time.Duration {
	return &dur
}
