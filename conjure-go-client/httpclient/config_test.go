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
	"testing"
	"time"

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
				URIs:         []string{"https://localhost"},
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
      http2-read-idle-timeout: 10s
      http2-ping-timeout: 5s
`,
			ExpectedConfig: ServicesConfig{
				Services: map[string]ClientConfig{
					"my-service": {
						APIToken:    &[]string{"so-secret"}[0],
						ReadTimeout: &[]time.Duration{time.Minute}[0],
					},
					"my-2nd-service": {
						APIToken:             &[]string{"so-secret-2"}[0],
						ReadTimeout:          &[]time.Duration{2 * time.Minute}[0],
						HTTP2ReadIdleTimeout: &[]time.Duration{10 * time.Second}[0],
						HTTP2PingTimeout:     &[]time.Duration{5 * time.Second}[0],
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
