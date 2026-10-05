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

package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime/debug"
	"testing"

	"github.com/palantir/pkg/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeDefaultUserAgent(t *testing.T) {
	for _, tc := range []struct {
		name     string
		info     *debug.BuildInfo
		ok       bool
		expected string
	}{
		{
			name:     "main module path without slash",
			info:     &debug.BuildInfo{Main: debug.Module{Path: "bar-service"}},
			ok:       true,
			expected: "bar-service",
		},
		{
			name:     "valid main module path",
			info:     &debug.BuildInfo{Main: debug.Module{Path: "github.com/foo/bar-service"}},
			ok:       true,
			expected: "bar-service",
		},
		{
			name:     "main module path with major version suffix",
			info:     &debug.BuildInfo{Main: debug.Module{Path: "github.com/foo/bar-service/v2"}},
			ok:       true,
			expected: "bar-service-v2",
		},
		{
			name:     "main module path with multi-digit major version suffix",
			info:     &debug.BuildInfo{Main: debug.Module{Path: "github.com/foo/bar-service/v10"}},
			ok:       true,
			expected: "bar-service-v10",
		},
		{
			name:     "main module path with non-version suffix",
			info:     &debug.BuildInfo{Main: debug.Module{Path: "github.com/foo/bar-service/v2beta"}},
			ok:       true,
			expected: "v2beta",
		},
		{
			name:     "build info unavailable",
			ok:       false,
			expected: fallbackUserAgent,
		},
		{
			name:     "empty main module path",
			info:     &debug.BuildInfo{Main: debug.Module{Path: ""}},
			ok:       true,
			expected: fallbackUserAgent,
		},
		{
			name:     "command-line-arguments main module path",
			info:     &debug.BuildInfo{Main: debug.Module{Path: "command-line-arguments"}},
			ok:       true,
			expected: fallbackUserAgent,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, computeDefaultUserAgent(tc.info, tc.ok))
		})
	}
}

func TestDefaultUserAgentMiddleware(t *testing.T) {
	var observedUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		observedUserAgent = req.Header.Get("User-Agent")
	}))
	defer server.Close()

	t.Run("no explicit user agent set", func(t *testing.T) {
		rootRegistry := metrics.NewRootMetricsRegistry()
		ctx := metrics.WithRegistry(context.Background(), rootRegistry)

		client, err := NewHTTPClient(WithServiceName("my-service"))
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
		require.NoError(t, err)
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, defaultUserAgent(), observedUserAgent)
		assert.NotEqual(t, "", observedUserAgent)

		var metricName string
		rootRegistry.Each(func(name string, _ metrics.Tags, _ metrics.MetricVal) {
			if name == MetricDefaultUserAgent {
				metricName = name
			}
		})
		assert.Equal(t, MetricDefaultUserAgent, metricName, "expected default user agent metric to be recorded")
	})

	t.Run("explicit user agent wins", func(t *testing.T) {
		rootRegistry := metrics.NewRootMetricsRegistry()
		ctx := metrics.WithRegistry(context.Background(), rootRegistry)

		client, err := NewHTTPClient(WithServiceName("my-service"), WithUserAgent("explicit-agent"))
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
		require.NoError(t, err)
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, "explicit-agent", observedUserAgent)

		rootRegistry.Each(func(name string, _ metrics.Tags, _ metrics.MetricVal) {
			assert.NotEqual(t, MetricDefaultUserAgent, name, "default user agent metric should not be recorded when caller sets an explicit User-Agent")
		})
	})
}
