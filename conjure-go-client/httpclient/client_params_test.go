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
	"crypto/tls"
	"fmt"
	"net/http"
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

	//refreshableCfg := refreshable.NewDefaultRefreshable(ClientConfig{
	//	ServiceName: "test",
	//	URIs:        []string{testAddr},
	//})

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
				transport := unwrapTransport(client.client.CurrentHTTPClient().Transport)
				assert.NotContains(t, transport.TLSClientConfig.NextProtos, "h2")
			},
		},
		{
			Name:  "NoProxy",
			Param: WithNoProxy(),
			Test: func(t *testing.T, client *clientImpl) {
				transport := unwrapTransport(client.client.CurrentHTTPClient().Transport)
				proxy := transport.Proxy
				assert.Nil(t, proxy)
			},
		},
		{
			Name:  "MaxIdleConns",
			Param: WithMaxIdleConns(100),
			Test: func(t *testing.T, client *clientImpl) {
				transport := unwrapTransport(client.client.CurrentHTTPClient().Transport)
				assert.Equal(t, 100, transport.MaxIdleConns)
			},
		},
		{
			Name:  "MaxIdleConnsPerHost",
			Param: WithMaxIdleConnsPerHost(50),
			Test: func(t *testing.T, client *clientImpl) {
				transport := unwrapTransport(client.client.CurrentHTTPClient().Transport)
				assert.Equal(t, 50, transport.MaxIdleConnsPerHost)
			},
		},
		{
			Name:  "ProxyURL",
			Param: WithProxyFromEnvironment(),
			Test: func(t *testing.T, client *clientImpl) {
				require.NoError(t, os.Setenv("https_proxy", testURL.String()))
				transport := unwrapTransport(client.client.CurrentHTTPClient().Transport)
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
				transport := unwrapTransport(client.client.CurrentHTTPClient().Transport)
				assert.True(t, transport.TLSClientConfig.InsecureSkipVerify, "InsecureSkipVerify should stay set")
			},
		},
		{
			Name:  "Nil TLSConfig",
			Param: WithTLSConfig(nil),
			Test: func(t *testing.T, client *clientImpl) {
				// No-op: passing nil should not cause panic
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
				transport := unwrapTransport(client.client.CurrentHTTPClient().Transport)
				assert.True(t, transport.TLSClientConfig.InsecureSkipVerify)
			},
		},
		//{
		//	Name:  "RefreshableConfig",
		//	Param: WithRefreshableConfig(NewRefreshingClientConfig(refreshableCfg)),
		//	Test: func(t *testing.T, client *clientImpl) {
		//		// check URIs is set prior to the change
		//		assert.Equal(t, []string{testAddr}, client.uris.CurrentStringSlice())
		//		// update the client config with a new URI
		//		newConfig := ClientConfig{
		//			ServiceName: "test",
		//			URIs:        []string{"https://changed-uri.local"},
		//		}
		//		err := refreshableCfg.Update(newConfig)
		//		require.NoError(t, err)
		//		assert.Equal(t, newConfig.URIs, client.uris.CurrentStringSlice(), "client URIs should be updated with the refreshed values")
		//	},
		//},
	} {
		t.Run(test.Name, func(t *testing.T) {
			client, err := NewClient(test.Param)
			require.NoError(t, err)
			test.Test(t, client.(*clientImpl))
		})
	}
}

func unwrapTransport(rt http.RoundTripper) *http.Transport {
	unwrapped := rt
	for {
		switch v := unwrapped.(type) {
		case *wrappedClient:
			unwrapped = v.baseTransport
		case *http.Transport:
			return v
		case *refreshingclient.RefreshableTransport:
			return v.Current().(*http.Transport)
		default:
			panic(fmt.Sprintf("unknown roundtripper type %T", unwrapped))
		}
	}
}
