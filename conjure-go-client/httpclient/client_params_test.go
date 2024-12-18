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

package httpclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient/internal/refreshingclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuilder(t *testing.T) {
	testAddr := "https://palantir.com"
	testURL, _ := url.Parse(testAddr)

	for _, test := range []struct {
		Name  string
		Param ClientParam
		Test  func(*testing.T, *clientImpl)
	}{
		{
			Name:  "HTTPTimeout",
			Param: WithHTTPTimeout(time.Hour),
			Test: func(t *testing.T, client *clientImpl) {
				assert.Equal(t, client.client.CurrentHTTPClient().Timeout, time.Hour)
			},
		},
		{
			Name:  "DisableHTTP2",
			Param: WithDisableHTTP2(),
			Test: func(t *testing.T, client *clientImpl) {
				transport, _ := unwrapTransport(client.client.CurrentHTTPClient().Transport)
				assert.NotContains(t, transport.TLSClientConfig.NextProtos, "h2")
			},
		},
		{
			Name:  "MaxIdleConns",
			Param: WithMaxIdleConns(100),
			Test: func(t *testing.T, client *clientImpl) {
				transport, _ := unwrapTransport(client.client.CurrentHTTPClient().Transport)
				assert.Equal(t, 100, transport.MaxIdleConns)
			},
		},
		{
			Name:  "MaxIdleConnsPerHost",
			Param: WithMaxIdleConnsPerHost(50),
			Test: func(t *testing.T, client *clientImpl) {
				transport, _ := unwrapTransport(client.client.CurrentHTTPClient().Transport)
				assert.Equal(t, 50, transport.MaxIdleConnsPerHost)
			},
		},
		{
			Name:  "ProxyFromEnvironment by default",
			Param: nil,
			Test: func(t *testing.T, client *clientImpl) {
				require.NoError(t, os.Setenv("https_proxy", testURL.String()))
				transport, _ := unwrapTransport(client.client.CurrentHTTPClient().Transport)
				resp, err := transport.Proxy(&http.Request{URL: testURL})
				require.NoError(t, err)
				require.NotNil(t, resp)
				require.Equal(t, testURL.String(), resp.String())
			},
		},
		{
			Name:  "NoProxy",
			Param: WithNoProxy(),
			Test: func(t *testing.T, client *clientImpl) {
				transport, _ := unwrapTransport(client.client.CurrentHTTPClient().Transport)
				proxy := transport.Proxy
				assert.Nil(t, proxy)
			},
		},
		{
			Name:  "ProxyFromEnvironment",
			Param: WithProxyFromEnvironment(),
			Test: func(t *testing.T, client *clientImpl) {
				require.NoError(t, os.Setenv("https_proxy", testURL.String()))
				transport, _ := unwrapTransport(client.client.CurrentHTTPClient().Transport)
				resp, err := transport.Proxy(&http.Request{URL: testURL})
				require.NoError(t, err)
				require.NotNil(t, resp)
				require.Equal(t, testURL.String(), resp.String())
			},
		},
		{
			Name:  "ProxyURL",
			Param: WithProxyURL(testURL.String()),
			Test: func(t *testing.T, client *clientImpl) {
				transport, _ := unwrapTransport(client.client.CurrentHTTPClient().Transport)
				resp, err := transport.Proxy(&http.Request{URL: testURL})
				require.NoError(t, err)
				require.NotNil(t, resp)
				require.Equal(t, testURL.String(), resp.String())
			},
		},
		{
			Name:  "TLSConfig",
			Param: WithTLSConfig(&tls.Config{InsecureSkipVerify: true}),
			Test: func(t *testing.T, client *clientImpl) {
				transport, _ := unwrapTransport(client.client.CurrentHTTPClient().Transport)
				assert.True(t, transport.TLSClientConfig.InsecureSkipVerify, "InsecureSkipVerify should stay set")
			},
		},
		{
			Name:  "Nil TLSConfig",
			Param: WithTLSConfig(nil),
			Test: func(t *testing.T, client *clientImpl) {
				// No-op: passing nil should not cause panic

				transport, _ := unwrapTransport(client.client.CurrentHTTPClient().Transport)
				assert.NotNil(t, transport.TLSClientConfig)
			},
		},
		{
			Name:  "UnlimitedRetries",
			Param: WithUnlimitedRetries(),
			Test: func(t *testing.T, client *clientImpl) {
				assert.Equal(t, 0, *client.maxAttempts.CurrentIntPtr())
			},
		},
		{
			Name:  "TLSInsecureSkipVerify",
			Param: WithTLSInsecureSkipVerify(),
			Test: func(t *testing.T, client *clientImpl) {
				transport, _ := unwrapTransport(client.client.CurrentHTTPClient().Transport)
				assert.True(t, transport.TLSClientConfig.InsecureSkipVerify)
			},
		},
		{
			Name: "TLSConfig from config",
			Param: WithConfig(ClientConfig{
				Security: SecurityConfig{
					InsecureSkipVerify: &[]bool{true}[0],
				},
			}),
			Test: func(t *testing.T, client *clientImpl) {
				transport, _ := unwrapTransport(client.client.CurrentHTTPClient().Transport)
				assert.True(t, transport.TLSClientConfig.InsecureSkipVerify)
			},
		},
	} {
		t.Run(test.Name, func(t *testing.T) {
			// Must provide URLs for client creation
			urls := WithBaseURLs([]string{"https://localhost"})
			client, err := NewClient(urls, test.Param)
			require.NoError(t, err)
			test.Test(t, client.(*clientImpl))
		})
	}
}

func TestMiddlewareOrdering(t *testing.T) {
	for _, tc := range []struct {
		Name          string
		Config        ClientConfig
		ClientParams  []ClientParam
		RequestParams []RequestParam
		ExpectHeaders http.Header
	}{
		{
			Name:          "no middleware",
			ExpectHeaders: http.Header{},
		},
		{
			Name:         "WithSetHeader middleware",
			ClientParams: []ClientParam{WithSetHeader("X-Test", "value1"), WithSetHeader("X-Test", "value2")},
			ExpectHeaders: http.Header{
				"X-Test": []string{"value2"},
			},
		},
		{
			Name:         "WithAddHeader middleware",
			ClientParams: []ClientParam{WithAddHeader("X-Test", "value1"), WithAddHeader("X-Test", "value2")},
			ExpectHeaders: http.Header{
				"X-Test": []string{"value1", "value2"},
			},
		},
		{
			Name:         "WithAddHeader adds to WithSetHeader middleware",
			ClientParams: []ClientParam{WithSetHeader("X-Test", "value1"), WithAddHeader("X-Test", "value2")},
			ExpectHeaders: http.Header{
				"X-Test": []string{"value1", "value2"},
			},
		},
		{
			Name:         "WithSetHeader overwrites WithAddHeader middleware",
			ClientParams: []ClientParam{WithAddHeader("X-Test", "value1"), WithSetHeader("X-Test", "value2")},
			ExpectHeaders: http.Header{
				"X-Test": []string{"value2"},
			},
		},
		{
			Name:          "WithHeader request param overwrites WithAddHeader middleware",
			ClientParams:  []ClientParam{WithAddHeader("X-Test", "value1")},
			RequestParams: []RequestParam{WithHeader("X-Test", "value2")},
			ExpectHeaders: http.Header{
				"X-Test": []string{"value2"},
			},
		},
		{
			Name: "WithInnerMiddleware overwrites WithAddHeader middleware",
			ClientParams: []ClientParam{
				WithAddHeader("X-Test", "value1"),
				WithInnerMiddleware(MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
					req.Header.Set("X-Test", "value2")
					return next.RoundTrip(req)
				})),
			},
			ExpectHeaders: http.Header{
				"X-Test": []string{"value2"},
			},
		},
		{
			Name: "WithAddHeader middleware adds to WithInnerMiddleware",
			ClientParams: []ClientParam{
				WithInnerMiddleware(MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
					req.Header.Set("X-Test", "value1")
					return next.RoundTrip(req)
				})),
				WithAddHeader("X-Test", "value2"),
			},
			ExpectHeaders: http.Header{
				"X-Test": []string{"value1", "value2"},
			},
		},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				delete(req.Header, "Accept-Encoding")
				delete(req.Header, "User-Agent")
				assert.Equal(t, tc.ExpectHeaders, req.Header)
			}))
			defer server.Close()
			cfg := tc.Config
			cfg.URIs = []string{server.URL}
			clientParams := []ClientParam{WithConfig(cfg)}
			for _, p := range tc.ClientParams {
				clientParams = append(clientParams, p)
			}

			client, err := NewClient(clientParams...)
			require.NoError(t, err)

			resp, err := client.Get(context.Background(), tc.RequestParams...)
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.EqualValues(t, http.StatusOK, resp.StatusCode)
		})
	}
}

func unwrapTransport(rt http.RoundTripper) (*http.Transport, []Middleware) {
	unwrapped := rt
	var middlewares []Middleware
	for {
		switch v := unwrapped.(type) {
		case *refreshingclient.RefreshableTransport:
			unwrapped = v.Current().(http.RoundTripper)
		case *wrappedClient:
			unwrapped = v.baseTransport
			middlewares = append(middlewares, v.middleware)
		case *http.Transport:
			return v, middlewares
		default:
			panic(fmt.Sprintf("unknown roundtripper type %T", unwrapped))
		}
	}
}
