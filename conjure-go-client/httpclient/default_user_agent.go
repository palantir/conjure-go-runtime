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
	"net/http"
	"runtime/debug"
	"sync"

	"github.com/palantir/pkg/metrics"
	"github.com/palantir/pkg/refreshable/v2"
)

const (
	// MetricDefaultUserAgent counts requests sent with the built-in default User-Agent
	// fallback because no explicit User-Agent was configured on the client.
	MetricDefaultUserAgent = "client.request.default-user-agent"

	fallbackUserAgent = "conjure-go-runtime"
)

// defaultUserAgent returns the User-Agent fallback for the running binary, computed once per process.
var defaultUserAgent = sync.OnceValue(func() string {
	info, ok := debug.ReadBuildInfo()
	return computeDefaultUserAgent(info, ok)
})

// computeDefaultUserAgent returns the main module path from info, or fallbackUserAgent if build info is unavailable.
func computeDefaultUserAgent(info *debug.BuildInfo, ok bool) string {
	// "command-line-arguments" is the virtual package path the go command synthesizes when
	// building/running a bare list of files instead of a package (e.g. "go run main.go"), so
	// it carries no useful service identity. See "Package lists and patterns" at
	// https://pkg.go.dev/cmd/go#hdr-Package_lists_and_patterns.
	if !ok || info == nil || info.Main.Path == "" || info.Main.Path == "command-line-arguments" {
		return fallbackUserAgent
	}
	return info.Main.Path
}

// newDefaultUserAgentMiddleware returns a Middleware that sets the User-Agent header to a
// value derived from the running binary's build info, but only if no User-Agent has already
// been set by an earlier middleware or client param.
func newDefaultUserAgentMiddleware(serviceName refreshable.Refreshable[string], disabledMetrics refreshable.Refreshable[bool]) Middleware {
	return &defaultUserAgentMiddleware{
		serviceName:     serviceName,
		disabledMetrics: disabledMetrics,
		userAgent:       defaultUserAgent(),
	}
}

type defaultUserAgentMiddleware struct {
	serviceName     refreshable.Refreshable[string]
	disabledMetrics refreshable.Refreshable[bool]
	userAgent       string
}

func (m *defaultUserAgentMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	if _, ok := req.Header["User-Agent"]; !ok {
		req.Header.Set("User-Agent", m.userAgent)
		if m.disabledMetrics == nil || !m.disabledMetrics.Current() {
			serviceNameTag := metrics.NewTagWithFallbackValue(MetricTagServiceName, m.serviceName.Current(), "unknown")
			metrics.FromContext(req.Context()).Counter(MetricDefaultUserAgent, serviceNameTag).Inc(1)
		}
	}
	return next.RoundTrip(req)
}
