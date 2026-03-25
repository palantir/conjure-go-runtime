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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/palantir/pkg/refreshable/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

func TestRefreshableClientConfig(t *testing.T) {
	const serviceName = "serviceName"

	t.Run("default static config", func(t *testing.T) {
		_, err := NewClient(WithConfig(ClientConfig{ServiceName: serviceName, URIs: []string{"https://localhost"}}))
		require.NoError(t, err)
	})

	// Build a refreshable client from the ground up -- start with an empty configuration, then add/mutate values.

	initialConfig := ServicesConfig{
		Default:  ClientConfig{},
		Services: map[string]ClientConfig{},
	}
	initialConfigBytes, err := yaml.Marshal(initialConfig)
	require.NoError(t, err)
	refreshableConfigBytes := refreshable.New(initialConfigBytes)
	updateRefreshableBytes := func(s ServicesConfig) {
		b, err := yaml.Marshal(s)
		if err != nil {
			panic(err)
		}
		refreshableConfigBytes.Update(b)
	}
	mapped, _ := refreshable.Map(refreshableConfigBytes, func(b []byte) ServicesConfig {
		var c ServicesConfig
		if err := yaml.Unmarshal(b, &c); err != nil {
			panic(err)
		}
		return c
	})
	refreshableServicesConfig := mapped

	t.Run("refreshable config without uris fails", func(t *testing.T) {
		refreshableClientConfig, unsubscribe := refreshable.Map(refreshableServicesConfig, func(t ServicesConfig) ClientConfig {
			return t.ClientConfig(serviceName)
		})
		t.Cleanup(unsubscribe)
		client, err := NewClientFromRefreshableConfig(context.Background(), refreshableClientConfig)
		require.EqualError(t, err, "httpclient URLs must not be empty")
		require.Nil(t, client)

		// Create test servers to verify URIs are set correctly
		server1 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			rw.WriteHeader(http.StatusOK)
		}))
		defer server1.Close()

		client, err = NewClientFromRefreshableConfig(context.Background(), refreshableClientConfig, WithBaseURLs([]string{server1.URL}))
		require.NoError(t, err, "expected to successfully create client using WithBaseURL even when config has no URIs")
		// Verify client works by making a request
		resp, reqErr := client.Get(context.Background())
		require.NoError(t, reqErr)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		client, err = NewClientFromRefreshableConfig(context.Background(), refreshableClientConfig, WithRefreshableBaseURLs(refreshable.New([]string{server1.URL})))
		require.NoError(t, err, "expected to successfully create client using WithRefreshableBaseURLs even when config has no URIs")
		resp, reqErr = client.Get(context.Background())
		require.NoError(t, reqErr)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		t.Run("WithAllowCreateWithEmptyURIs", func(t *testing.T) {
			client, err := NewClientFromRefreshableConfig(context.Background(), refreshableClientConfig, WithAllowCreateWithEmptyURIs())
			require.NoError(t, err, "expected to create a client from empty client config with WithAllowCreateWithEmptyURIs")

			// Expect error making request
			_, err = client.Get(context.Background())
			require.Error(t, err)

			// Update config to add URIs
			initialConfig.Services[serviceName] = ClientConfig{ServiceName: serviceName, URIs: []string{server1.URL}}
			updateRefreshableBytes(initialConfig)

			// Verify client now works with the updated URIs
			assert.Eventually(t, func() bool {
				resp, err := client.Get(context.Background())
				return err == nil && resp.StatusCode == http.StatusOK
			}, time.Second*2, time.Millisecond*100, "expected client to work after URIs are updated")
		})
	})

	refreshableClientConfig, unsubscribe := refreshable.Map(refreshableServicesConfig, func(t ServicesConfig) ClientConfig {
		return t.ClientConfig(serviceName)
	})
	t.Cleanup(unsubscribe)

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	initialConfig.Services[serviceName] = ClientConfig{ServiceName: serviceName, URIs: []string{server.URL}}
	updateRefreshableBytes(initialConfig)
	client, err := NewClientFromRefreshableConfig(context.Background(), refreshableClientConfig)
	require.NoError(t, err, "expected to create a client from empty client config")

	t.Run("default refreshable config", func(t *testing.T) {
		// Verify the client works with default config
		resp, err := client.Get(context.Background())
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("update timeout succeeds", func(t *testing.T) {
		t.Run("service config", func(t *testing.T) {
			serviceCfg := initialConfig.Services[serviceName]
			serviceCfg.WriteTimeout = new(time.Second)
			initialConfig.Services[serviceName] = serviceCfg
			updateRefreshableBytes(initialConfig)

			// Verify the client still works after timeout update
			resp, err := client.Get(context.Background())
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode)
		})
		t.Run("default config", func(t *testing.T) {
			initialConfig.Default.ReadTimeout = new(time.Hour)
			updateRefreshableBytes(initialConfig)

			resp, err := client.Get(context.Background())
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode)
		})
		t.Run("unset, falls back to default", func(t *testing.T) {
			initialConfig.Default.ReadTimeout = nil
			serviceCfg := initialConfig.Services[serviceName]
			serviceCfg.WriteTimeout = nil
			initialConfig.Services[serviceName] = serviceCfg
			updateRefreshableBytes(initialConfig)

			resp, err := client.Get(context.Background())
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode)
		})
	})

	t.Run("update dial timeout", func(t *testing.T) {
		connTimeout := time.Nanosecond
		initialConfig.Default.ConnectTimeout = &connTimeout
		updateRefreshableBytes(initialConfig)

		// Reset after test
		defer func() {
			initialConfig.Default.ConnectTimeout = nil
			updateRefreshableBytes(initialConfig)
		}()

		// With a 1ns dial timeout, connecting to an external host should fail quickly.
		// Use a client with a different URI that requires a real TCP connection.
		dialTestConfig := refreshable.New(ClientConfig{
			ServiceName:    serviceName,
			URIs:           []string{"https://palantir.com"},
			ConnectTimeout: &connTimeout,
			MaxNumRetries:  new(int), // 0 retries
		})
		dialClient, err := NewClientFromRefreshableConfig(context.Background(), dialTestConfig)
		require.NoError(t, err)
		start := time.Now()
		_, dialErr := dialClient.Get(context.Background())
		require.Error(t, dialErr)
		assert.Less(t, time.Since(start), 5*time.Second, "Dial should fail quickly due to the 1ns timeout")
	})
}
