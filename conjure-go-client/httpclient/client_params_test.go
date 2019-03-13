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
				assert.Equal(t, client.client.Timeout, time.Hour)
			},
		},
		{
			Name:  "DisableHTTP2",
			Param: WithDisableHTTP2(),
			Test: func(t *testing.T, client *clientImpl) {
				transport := unwrapTransport(client.client.Transport)
				assert.NotContains(t, transport.TLSClientConfig.NextProtos, "h2")
			},
		},
		{
			Name:  "NoProxy",
			Param: WithNoProxy(),
			Test: func(t *testing.T, client *clientImpl) {
				transport := unwrapTransport(client.client.Transport)
				proxy := transport.Proxy
				assert.Nil(t, proxy)
			},
		},
		{
			Name:  "MaxIdleConns",
			Param: WithMaxIdleConns(100),
			Test: func(t *testing.T, client *clientImpl) {
				transport := unwrapTransport(client.client.Transport)
				assert.Equal(t, 100, transport.MaxIdleConns)
			},
		},
		{
			Name:  "MaxIdleConnsPerHost",
			Param: WithMaxIdleConnsPerHost(50),
			Test: func(t *testing.T, client *clientImpl) {
				transport := unwrapTransport(client.client.Transport)
				assert.Equal(t, 50, transport.MaxIdleConnsPerHost)
			},
		},
		{
			Name:  "ProxyURL",
			Param: WithProxyFromEnvironment(),
			Test: func(t *testing.T, client *clientImpl) {
				require.NoError(t, os.Setenv("http_proxy", testURL.String()))
				transport := unwrapTransport(client.client.Transport)
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
				transport := unwrapTransport(client.client.Transport)
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
		default:
			panic(fmt.Sprintf("unknown roundtripper type %T", unwrapped))
		}
	}
}
