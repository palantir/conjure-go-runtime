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
	"net/http"
	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient/internal/refreshingclient"
	"github.com/palantir/pkg/metrics"
	"github.com/palantir/pkg/refreshable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

func TestServicesConfig(t *testing.T) {
	for _, test := range []struct {
		Name           string
		ServiceName    string
		Config         ServicesConfig
		ExpectedConfig ClientConfig
	}{
		{
			Name:        "defaults",
			ServiceName: "my-service",
			Config: ServicesConfig{
				Default: ClientConfig{
					ReadTimeout: &[]time.Duration{time.Minute}[0],
				},
				Services: map[string]ClientConfig{
					"my-service": {
						APIToken: &[]string{"so-secret"}[0],
					},
				},
			},
			ExpectedConfig: ClientConfig{
				ServiceName: "my-service",
				APIToken:    &[]string{"so-secret"}[0],
				ReadTimeout: &[]time.Duration{time.Minute}[0],
			},
		},
	} {
		t.Run(test.Name, func(t *testing.T) {
			actual := test.Config.ClientConfig(test.ServiceName)
			require.Equal(t, test.ExpectedConfig, actual)
		})
	}
}

func TestWithConfigParam(t *testing.T) {
	conf := ServicesConfig{
		Services: map[string]ClientConfig{
			"my-service": {
				ReadTimeout:  &[]time.Duration{2 * time.Second}[0],
				WriteTimeout: &[]time.Duration{3 * time.Second}[0],
			},
		},
	}
	client, err := NewClient(WithConfig(conf.ClientConfig("my-service")))
	require.NoError(t, err)
	assert.Equal(t, 3*time.Second, client.(*clientImpl).client.CurrentHTTPClient().Timeout)
}

func TestWithConfigForHTTPClientParam(t *testing.T) {
	conf := ServicesConfig{
		Services: map[string]ClientConfig{
			"my-service": {
				ReadTimeout:  &[]time.Duration{2 * time.Second}[0],
				WriteTimeout: &[]time.Duration{3 * time.Second}[0],
			},
		},
	}
	client, err := NewHTTPClient(WithConfigForHTTPClient(conf.ClientConfig("my-service")))
	require.NoError(t, err)
	assert.Equal(t, 3*time.Second, client.Timeout)
}

func TestConfigYAML(t *testing.T) {
	for _, test := range []struct {
		Name               string
		ServicesConfigYAML string
		ExpectedConfig     ServicesConfig
	}{
		{
			Name: "empty defaults",
			ServicesConfigYAML: `
clients:
  services:
    my-service:
      api-token: so-secret
      read-timeout: 1m
    my-2nd-service:
      api-token: so-secret-2
      read-timeout: 2m
`,
			ExpectedConfig: ServicesConfig{
				Services: map[string]ClientConfig{
					"my-service": {
						APIToken:    &[]string{"so-secret"}[0],
						ReadTimeout: &[]time.Duration{time.Minute}[0],
					},
					"my-2nd-service": {
						APIToken:    &[]string{"so-secret-2"}[0],
						ReadTimeout: &[]time.Duration{2 * time.Minute}[0],
					},
				},
			},
		},
	} {
		t.Run(test.Name, func(t *testing.T) {
			var actual struct {
				Clients ServicesConfig `yaml:"clients"`
			}
			err := yaml.UnmarshalStrict([]byte(test.ServicesConfigYAML), &actual)
			require.NoError(t, err)
			require.Equal(t, test.ExpectedConfig, actual.Clients)
		})
	}
}

func TestRefreshableClientConfig(t *testing.T) {
	const serviceName = "serviceName"
	initialConfig := ServicesConfig{
		Default:  ClientConfig{},
		Services: map[string]ClientConfig{},
	}
	initialConfigBytes, err := yaml.Marshal(initialConfig)
	require.NoError(t, err)
	refreshableConfigBytes := refreshable.NewDefaultRefreshable(initialConfigBytes)
	//updateRefreshableBytes := func(s ServicesConfig) {
	//	b, err := yaml.Marshal(s)
	//	if err != nil {
	//		panic(err)
	//	}
	//	if err :=  refreshableConfigBytes.Update(b); err != nil {
	//		panic(err)
	//	}
	//}
	refreshableServicesConfig := NewRefreshingServicesConfig(refreshableConfigBytes.Map(func(i interface{}) interface{} {
		var c ServicesConfig
		if err := yaml.Unmarshal(i.([]byte), &c); err != nil {
			panic(err)
		}
		return c
	}))
	refreshableClientConfig := RefreshableClientConfigFromServiceConfig(refreshableServicesConfig, serviceName)
	client, err := NewClientFromRefreshableConfig(context.Background(), refreshableClientConfig)
	require.NoError(t, err, "expected to create a client from empty client config")

	retryParams := client.(*clientImpl).backoffOptions

	currentHTTPClient := func() *http.Client {
		return client.(*clientImpl).client.CurrentHTTPClient()
	}
	currentHTTPTransport := func() (*http.Transport, []Middleware) {
		c := currentHTTPClient()
		unwrapped := c.Transport
		var middlewares []Middleware
		for {
			switch r := unwrapped.(type) {
			case *wrappedClient:
				unwrapped = r.baseTransport
				middlewares = append(middlewares, r.middleware)
			case *refreshingclient.RefreshableTransport:
				unwrapped = r.Current().(http.RoundTripper)
			case *http.Transport:
				return r, middlewares
			default:
				panic(r)
			}
		}
	}
	var (
		recoveryM recoveryMiddleware
		traceM    traceMiddleware
		metricsM  *metricsMiddleware
	)
	t.Run("default config", func(t *testing.T) {
		assert.Equal(t, defaultHTTPTimeout, currentHTTPClient().Timeout, "http timeout not set to default")

		assert.Nil(t, retryParams.MaxAttempts.CurrentIntPtr())
		assert.Equal(t, defaultInitialBackoff, retryParams.InitialBackoff.CurrentDuration())
		assert.Equal(t, defaultMaxBackoff, retryParams.MaxBackoff.CurrentDuration())

		initialTransport, initialMiddlewares := currentHTTPTransport()
		assert.Equal(t, defaultMaxIdleConns, initialTransport.MaxIdleConns)
		assert.Equal(t, defaultMaxIdleConnsPerHost, initialTransport.MaxIdleConnsPerHost)
		assert.Equal(t, defaultIdleConnTimeout, initialTransport.IdleConnTimeout)
		assert.Equal(t, defaultExpectContinueTimeout, initialTransport.ExpectContinueTimeout)
		assert.Equal(t, defaultTLSHandshakeTimeout, initialTransport.TLSHandshakeTimeout)
		assert.Equal(t, false, initialTransport.DisableKeepAlives)

		if assert.Len(t, initialMiddlewares, 3) {
			if assert.IsType(t, recoveryMiddleware{}, initialMiddlewares[0]) {
				recoveryM = initialMiddlewares[0].(recoveryMiddleware)
				assert.False(t, recoveryM.Disabled.CurrentBool())
			}
			if assert.IsType(t, traceMiddleware{}, initialMiddlewares[1]) {
				traceM = initialMiddlewares[1].(traceMiddleware)
				assert.True(t, traceM.CreateRequestSpan.CurrentBool())
				assert.True(t, traceM.InjectHeaders.CurrentBool())
			}
			if assert.IsType(t, &metricsMiddleware{}, initialMiddlewares[2]) {
				metricsM = initialMiddlewares[2].(*metricsMiddleware)
				assert.False(t, metricsM.Disabled.CurrentBool())
				assert.Equal(t, metrics.MustNewTag(MetricTagServiceName, serviceName), metricsM.ServiceNameTag)
			}
		}
	})

	//&http.Transport{
	//	Proxy:                  nil,
	//	DialContext:            nil,
	//	Dial:                   nil,
	//	DialTLSContext:         nil,
	//	DialTLS:                nil,
	//	TLSClientConfig:        nil,
	//	DisableCompression:     false,
	//	ResponseHeaderTimeout:  0,
	//	ExpectContinueTimeout:  0,
	//	TLSNextProto:           nil,
	//	ProxyConnectHeader:     nil,
	//	GetProxyConnectHeader:  nil,
	//	MaxResponseHeaderBytes: 0,
	//	WriteBufferSize:        0,
	//	ReadBufferSize:         0,
	//	ForceAttemptHTTP2:      false,
	//}
	//
	//defaultDialTimeout           = 10 * time.Second
	//defaultKeepAlive             = 30 * time.Second

	//for _, test := range []struct {
	//	Name        string
	//	ServiceName string
	//	Configs     []ServicesConfig
	//	Verify      []func(t *testing.T, c *clientImpl)
	//}{} {
	//	t.Run(test.Name, func(t *testing.T) {
	//	})
	//}
}
