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
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoundTripperWithToken(t *testing.T) {
	var wrappedRTInvoked bool
	tokenProvider := httpclient.TokenProvider(func(_ context.Context) (string, error) {
		return "foo", nil
	})

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		wrappedRTInvoked = true
		assert.Equal(t, "Bearer foo", req.Header.Get("Authorization"))
		rw.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := httpclient.NewClient(
		httpclient.WithHTTPTimeout(time.Minute),
		httpclient.WithAuthTokenProvider(tokenProvider),
		httpclient.WithBaseURLs([]string{server.URL}))
	require.NoError(t, err)

	resp, err := client.Do(context.Background(), httpclient.WithRequestMethod(http.MethodGet))
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, wrappedRTInvoked)
}

func TestRoundTripperWithBasicAuth(t *testing.T) {
	var wrappedRTInvoked bool
	expected := httpclient.BasicAuth{
		User:     "user",
		Password: "password",
	}

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		wrappedRTInvoked = true
		user, pass, ok := req.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, expected.User, user)
		assert.Equal(t, expected.Password, pass)
		rw.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := httpclient.NewClient(
		httpclient.WithHTTPTimeout(time.Minute),
		httpclient.WithBasicAuth(expected.User, expected.Password),
		httpclient.WithBaseURLs([]string{server.URL}))
	require.NoError(t, err)

	resp, err := client.Do(context.Background(), httpclient.WithRequestMethod(http.MethodGet))
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, wrappedRTInvoked)

}

func TestRoundTripperWithBasicAuthProvider(t *testing.T) {
	var wrappedRTInvoked bool
	expected := httpclient.BasicAuth{
		User:     "user",
		Password: "password",
	}

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		wrappedRTInvoked = true
		user, pass, ok := req.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, expected.User, user)
		assert.Equal(t, expected.Password, pass)
		rw.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := httpclient.NewClient(
		httpclient.WithHTTPTimeout(time.Minute),
		httpclient.WithBasicAuthProvider(func(ctx context.Context) (httpclient.BasicAuth, error) {
			return httpclient.BasicAuth{
				User:     "user",
				Password: "password",
			}, nil
		}),
		httpclient.WithBaseURLs([]string{server.URL}))
	require.NoError(t, err)

	resp, err := client.Do(context.Background(), httpclient.WithRequestMethod(http.MethodGet))
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, wrappedRTInvoked)
}

func TestAuthHeaders(t *testing.T) {
	username, password, token := "user", "pass", "eyJ..."
	apiTokenFile := filepath.Join(t.TempDir(), "token.txt")
	require.NoError(t, os.WriteFile(apiTokenFile, []byte(token), 0600))

	noAuthServer := func(t *testing.T) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			if assert.NotContains(t, req.Header, "Authorization") {
				rw.WriteHeader(http.StatusOK)
			} else {
				rw.WriteHeader(http.StatusUnauthorized)
			}
		}))
	}
	basicAuthServer := func(t *testing.T) *httptest.Server {
		basicAuthHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
		return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			if assert.Equal(t, basicAuthHeader, req.Header.Get("Authorization")) {
				rw.WriteHeader(http.StatusOK)
			} else {
				rw.WriteHeader(http.StatusUnauthorized)
			}
		}))
	}
	bearerAuthServer := func(t *testing.T) *httptest.Server {
		bearerAuthHeader := "Bearer " + token
		return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			if assert.Equal(t, bearerAuthHeader, req.Header.Get("Authorization")) {
				rw.WriteHeader(http.StatusOK)
			} else {
				rw.WriteHeader(http.StatusUnauthorized)
			}
		}))
	}

	for _, tc := range []struct {
		Name          string
		Server        func(t *testing.T) *httptest.Server
		Config        httpclient.ClientConfig
		ClientParams  []httpclient.ClientOrHTTPClientParam
		RequestParams []httpclient.RequestParam
	}{
		{
			Name:   "NoAuth",
			Server: noAuthServer,
		},
		// Basic Auth
		{
			Name:   "BasicAuth config",
			Server: basicAuthServer,
			Config: httpclient.ClientConfig{BasicAuth: &httpclient.BasicAuth{User: username, Password: password}},
		},
		{
			Name:         "WithBasicAuth param",
			Server:       basicAuthServer,
			ClientParams: []httpclient.ClientOrHTTPClientParam{httpclient.WithBasicAuth(username, password)},
		},
		{
			Name:   "WithBasicAuthProvider param",
			Server: basicAuthServer,
			ClientParams: []httpclient.ClientOrHTTPClientParam{httpclient.WithBasicAuthProvider(func(ctx context.Context) (httpclient.BasicAuth, error) {
				return httpclient.BasicAuth{User: username, Password: password}, nil
			})},
		},
		{
			Name:   "WithBasicAuthOptionalProvider present",
			Server: basicAuthServer,
			ClientParams: []httpclient.ClientOrHTTPClientParam{httpclient.WithBasicAuthOptionalProvider(func(ctx context.Context) (*httpclient.BasicAuth, error) {
				return &httpclient.BasicAuth{User: username, Password: password}, nil
			})},
		},
		{
			Name:   "WithBasicAuthOptionalProvider absent",
			Server: noAuthServer,
			ClientParams: []httpclient.ClientOrHTTPClientParam{httpclient.WithBasicAuthOptionalProvider(func(ctx context.Context) (*httpclient.BasicAuth, error) {
				return nil, nil
			})},
		},
		// Bearer tokens
		{
			Name:   "APIToken config",
			Server: bearerAuthServer,
			Config: httpclient.ClientConfig{APIToken: &token},
		},
		{
			Name:   "APITokenFile config",
			Server: bearerAuthServer,
			Config: httpclient.ClientConfig{APITokenFile: &apiTokenFile},
		},
		{
			Name:         "WithAuthToken param",
			Server:       bearerAuthServer,
			ClientParams: []httpclient.ClientOrHTTPClientParam{httpclient.WithAuthToken(token)},
		},
		{
			Name:   "WithAuthTokenProvider param",
			Server: bearerAuthServer,
			ClientParams: []httpclient.ClientOrHTTPClientParam{httpclient.WithAuthTokenProvider(func(ctx context.Context) (string, error) {
				return token, nil
			})},
		},
		// WithRequest*
		{
			Name:          "WithRequestBasicAuth param",
			Server:        basicAuthServer,
			RequestParams: []httpclient.RequestParam{httpclient.WithRequestBasicAuth(username, password)},
		},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			t.Run("httpclient.Client", func(t *testing.T) {
				server := tc.Server(t)
				defer server.Close()
				cfg := tc.Config
				cfg.URIs = []string{server.URL}
				clientParams := []httpclient.ClientParam{httpclient.WithConfig(cfg)}
				for _, p := range tc.ClientParams {
					clientParams = append(clientParams, p)
				}

				client, err := httpclient.NewClient(clientParams...)
				require.NoError(t, err)

				var requestParams []httpclient.RequestParam
				for _, p := range tc.RequestParams {
					requestParams = append(requestParams, p)
				}

				resp, err := client.Get(context.Background(), requestParams...)
				require.NoError(t, err)
				require.NotNil(t, resp)
				require.NoError(t, resp.Body.Close())
				require.EqualValues(t, 200, resp.StatusCode)
			})

			// if testing request params, don't run the *http.Client test
			if tc.RequestParams == nil {
				t.Run("*http.Client", func(t *testing.T) {
					server := tc.Server(t)
					defer server.Close()
					cfg := tc.Config
					clientParams := []httpclient.HTTPClientParam{httpclient.WithConfigForHTTPClient(cfg)}
					for _, p := range tc.ClientParams {
						clientParams = append(clientParams, p)
					}

					client, err := httpclient.NewHTTPClient(clientParams...)
					require.NoError(t, err)

					resp, err := client.Get(server.URL)
					require.NoError(t, err)
					require.NotNil(t, resp)
					require.NoError(t, resp.Body.Close())
					require.EqualValues(t, 200, resp.StatusCode)
				})
			}
		})
	}
}
