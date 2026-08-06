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
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient"
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
		{
			Name:         "WithBasicAuth param beats basic config",
			Server:       basicAuthServer,
			Config:       httpclient.ClientConfig{BasicAuth: &httpclient.BasicAuth{User: "wrong", Password: "wrong"}},
			ClientParams: []httpclient.ClientOrHTTPClientParam{httpclient.WithBasicAuth(username, password)},
		},
		{
			Name:         "WithBasicAuth param beats bearer config",
			Server:       basicAuthServer,
			Config:       httpclient.ClientConfig{APIToken: &token},
			ClientParams: []httpclient.ClientOrHTTPClientParam{httpclient.WithBasicAuth(username, password)},
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
			Name:         "WithAuthToken param beats basic config",
			Server:       bearerAuthServer,
			Config:       httpclient.ClientConfig{BasicAuth: &httpclient.BasicAuth{User: "wrong", Password: "wrong"}},
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

// Verifies that the auth middleware does not re-attach the Authorization header
// when the stdlib follows a cross-host redirect.
func TestAuthHeaderNotLeakedOnCrossHostRedirect(t *testing.T) {
	const (
		token    = "token"
		username = "user"
		password = "pass"
	)
	for _, tc := range []struct {
		name   string
		param  httpclient.ClientOrHTTPClientParam
		expect string
	}{
		{
			name:   "WithAuthToken",
			param:  httpclient.WithAuthToken(token),
			expect: "Bearer " + token,
		},
		{
			name:   "WithBasicAuth",
			param:  httpclient.WithBasicAuth(username, password),
			expect: "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password)),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var redirectCalled bool
			var redirectAuthValue string
			redirectHost := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				redirectCalled = true
				redirectAuthValue = req.Header.Get("Authorization")
				rw.WriteHeader(http.StatusOK)
			}))
			defer redirectHost.Close()

			// redirect from 127.0.0.1 to "localhost" so the target differs but is still reachable
			redirectURL := strings.Replace(redirectHost.URL, "127.0.0.1", "localhost", 1)
			require.NotEqual(t, redirectHost.URL, redirectURL, "expected httptest server to bind 127.0.0.1")

			var originAuthValue string
			origin := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				originAuthValue = req.Header.Get("Authorization")
				http.Redirect(rw, req, redirectURL, http.StatusFound)
			}))
			defer origin.Close()

			client, err := httpclient.NewClient(
				httpclient.WithBaseURLs([]string{origin.URL}),
				httpclient.WithHTTPTimeout(time.Minute),
				tc.param,
			)
			require.NoError(t, err)

			resp, err := client.Get(context.Background())
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())

			// Only the origin should see the Authorization header
			assert.Equal(t, tc.expect, originAuthValue)
			assert.True(t, redirectCalled)
			assert.Empty(t, redirectAuthValue)
		})
	}
}

func TestAuthHeaderNotLeakedOnCrossPortRedirect(t *testing.T) {
	const token = "token"
	var redirectAuthValue string
	target := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		redirectAuthValue = req.Header.Get("Authorization")
		rw.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		http.Redirect(rw, req, target.URL, http.StatusFound)
	}))
	defer origin.Close()

	client, err := httpclient.NewClient(
		httpclient.WithBaseURLs([]string{origin.URL}),
		httpclient.WithAuthToken(token),
	)
	require.NoError(t, err)

	resp, err := client.Get(t.Context())
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Empty(t, redirectAuthValue)
}

// Verifies that following a same-host redirect still attaches the Authorization header.
func TestAuthHeaderPreservedOnSameHostRedirect(t *testing.T) {
	const token = "token"

	var redirectCalled bool
	var authValue string
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/redirect" {
			redirectCalled = true
			authValue = req.Header.Get("Authorization")
			rw.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(rw, req, "/redirect", http.StatusFound)
	}))
	defer server.Close()

	client, err := httpclient.NewClient(
		httpclient.WithBaseURLs([]string{server.URL}),
		httpclient.WithHTTPTimeout(time.Minute),
		httpclient.WithAuthToken(token),
	)
	require.NoError(t, err)

	resp, err := client.Get(context.Background())
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	assert.True(t, redirectCalled)
	assert.Equal(t, "Bearer "+token, authValue)
}

func TestSensitiveHeadersNotReattachedOnCrossHostRedirect(t *testing.T) {
	testRedirectSensitiveHeaders(t, true)
}

func TestSensitiveHeadersPreservedOnSameHostRedirect(t *testing.T) {
	testRedirectSensitiveHeaders(t, false)
}

func TestRedirectTargetCookiesPreserved(t *testing.T) {
	var targetCookies []*http.Cookie
	target := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		targetCookies = req.Cookies()
		rw.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	targetURL, err := url.Parse(strings.Replace(target.URL, "127.0.0.1", "localhost", 1))
	require.NoError(t, err)

	origin := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		http.Redirect(rw, req, targetURL.String(), http.StatusFound)
	}))
	defer origin.Close()

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	jar.SetCookies(targetURL, []*http.Cookie{{Name: "target", Value: "cookie"}})
	client, err := httpclient.NewHTTPClient(httpclient.WithAddHeader("Cookie", "origin=secret"))
	require.NoError(t, err)
	client.Jar = jar

	resp, err := client.Get(origin.URL)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	if assert.Len(t, targetCookies, 1) {
		assert.Equal(t, "target", targetCookies[0].Name)
		assert.Equal(t, "cookie", targetCookies[0].Value)
	}
}

func testRedirectSensitiveHeaders(t *testing.T, crossHost bool) {
	const secret = "secret"
	for _, tc := range []struct {
		name  string
		key   string
		param httpclient.ClientOrHTTPClientParam
	}{
		{
			name:  "set authorization header",
			key:   "Authorization",
			param: httpclient.WithSetHeader("Authorization", secret),
		},
		{
			name:  "add cookie header",
			key:   "Cookie",
			param: httpclient.WithAddHeader("Cookie", secret),
		},
		{
			name: "custom middleware sets proxy authorization",
			key:  "Proxy-Authorization",
			param: httpclient.WithMiddleware(httpclient.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
				req.Header["proxy-authorization"] = []string{secret}
				return next.RoundTrip(req)
			})),
		},
	} {
		for _, useHTTPClient := range []bool{false, true} {
			clientName := "Client"
			if useHTTPClient {
				clientName = "HTTPClient"
			}
			t.Run(tc.name+"/"+clientName, func(t *testing.T) {
				var originValues, redirectValues []string
				redirectServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
					redirectValues = req.Header.Values(tc.key)
					rw.WriteHeader(http.StatusOK)
				}))
				defer redirectServer.Close()

				redirectURL := redirectServer.URL + "/redirect"
				if crossHost {
					redirectURL = strings.Replace(redirectURL, "127.0.0.1", "localhost", 1)
					require.NotEqual(t, redirectServer.URL+"/redirect", redirectURL)
				}

				origin := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
					originValues = req.Header.Values(tc.key)
					if crossHost {
						http.Redirect(rw, req, redirectURL, http.StatusFound)
						return
					}
					if req.URL.Path == "/redirect" {
						redirectValues = req.Header.Values(tc.key)
						rw.WriteHeader(http.StatusOK)
						return
					}
					http.Redirect(rw, req, "/redirect", http.StatusFound)
				}))
				defer origin.Close()

				if useHTTPClient {
					client, err := httpclient.NewHTTPClient(
						httpclient.WithHTTPTimeout(time.Minute),
						tc.param,
					)
					require.NoError(t, err)
					resp, err := client.Get(origin.URL)
					require.NoError(t, err)
					require.NoError(t, resp.Body.Close())
				} else {
					client, err := httpclient.NewClient(
						httpclient.WithBaseURLs([]string{origin.URL}),
						httpclient.WithHTTPTimeout(time.Minute),
						tc.param,
					)
					require.NoError(t, err)
					resp, err := client.Get(context.Background())
					require.NoError(t, err)
					require.NoError(t, resp.Body.Close())
				}

				assert.Contains(t, originValues, secret)
				if crossHost {
					assert.Empty(t, redirectValues)
				} else {
					assert.Contains(t, redirectValues, secret)
				}
			})
		}
	}
}
