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

package httpc

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptrace"
	"time"

	"github.com/palantir/pkg/metrics"
	"github.com/palantir/pkg/refreshable/v2"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/palantir/witchcraft-go-logging/wlog/svclog/svc1log"
)

const (
	metricTagServiceName  = "service-name"
	metricTagFamily       = "family"
	metricTagMethod       = "method"
	metricTagMethodName   = "method-name"
	metricTagNetwork      = "network"
	metricTagCipher       = "cipher"
	metricTagNextProtocol = "next_protocol"
	metricTagTLSVersion   = "tls_version"
)

var (
	metricTagConnectionNew    = metrics.MustNewTag("reused", "false")
	metricTagConnectionReused = metrics.MustNewTag("reused", "true")

	metricTagFamily1xx     = metrics.MustNewTag(metricTagFamily, "1xx")
	metricTagFamily2xx     = metrics.MustNewTag(metricTagFamily, "2xx")
	metricTagFamily3xx     = metrics.MustNewTag(metricTagFamily, "3xx")
	metricTagFamily4xx     = metrics.MustNewTag(metricTagFamily, "4xx")
	metricTagFamily5xx     = metrics.MustNewTag(metricTagFamily, "5xx")
	metricTagFamilyOther   = metrics.MustNewTag(metricTagFamily, "other")
	metricTagFamilyTimeout = metrics.MustNewTag(metricTagFamily, "timeout")
)

// TagsProvider produces metric tags from an HTTP request/response pair.
type TagsProvider interface {
	Tags(req *http.Request, resp *http.Response, err error) metrics.Tags
}

// TagsProviderFunc adapts a function to TagsProvider.
type TagsProviderFunc func(req *http.Request, resp *http.Response, err error) metrics.Tags

func (f TagsProviderFunc) Tags(req *http.Request, resp *http.Response, err error) metrics.Tags {
	return f(req, resp, err)
}

// StaticTagsProvider attaches the same tags to every request.
type StaticTagsProvider metrics.Tags

func (s StaticTagsProvider) Tags(req *http.Request, resp *http.Response, err error) metrics.Tags {
	return metrics.Tags(s)
}

// metricsMiddleware emits the full catalog of client metrics (see the constants above).
type metricsMiddleware struct {
	disabled    refreshable.Refreshable[bool]
	serviceName refreshable.Refreshable[string]
	tags        []TagsProvider
}

// MetricsMiddleware is a standalone constructor for [metricsMiddleware] for
// callers composing the middleware stack manually. Clients built via [Builder]
// install this automatically.
func MetricsMiddleware(serviceName string, tagProviders ...TagsProvider) Middleware {
	return &metricsMiddleware{serviceName: refreshable.New(serviceName), tags: tagProviders}
}

func (m *metricsMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (resp *http.Response, err error) {
	if m.disabled == nil || !m.disabled.Current() {
		newReq, callback := NewMetricsResponseCallback(req, m.serviceName.Current(), false, m.tags...)
		defer func() {
			callback(resp, err)
		}()
		req = newReq
	}
	return next.RoundTrip(req)
}

func NewMetricsResponseCallback(req *http.Request, serviceName string, disableClientTrace bool, tagsProviders ...TagsProvider) (*http.Request, func(resp *http.Response, err error)) {
	const (
		metricClientResponse  = "client.response"          // Timer; full round-trip; +method, method-name, family
		metricRequestInFlight = "client.request.in-flight" // Counter; concurrent requests
	)
	ctx := req.Context()
	registry := metrics.FromContext(metrics.AddTags(ctx, metrics.NewTagWithFallbackValue(metricTagServiceName, serviceName, "unknown")))
	if !disableClientTrace {
		ctx = httptrace.WithClientTrace(ctx, NewMetricsClientTrace(registry, svc1log.FromContext(ctx)))
		req = req.WithContext(ctx)
	}

	registry.Counter(metricRequestInFlight).Inc(1)
	start := time.Now()
	return req, func(resp *http.Response, err error) {
		registry.Counter(metricRequestInFlight).Dec(1)
		registry.Timer(metricClientResponse, responseMetricTags(tagsProviders, req, resp, err)...).UpdateSince(start)
	}
}

func NewMetricsClientTrace(registry metrics.Registry, logger svc1log.Logger) *httptrace.ClientTrace {
	const (
		metricConnCreate          = "client.connection.create"            // Counter; +reused (true/false)
		metricConnAcquire         = "client.connection.acquire"           // Timer; GetConn → GotConn; +reused
		metricConnIdleReturnError = "client.connection.idle-return-error" // Meter; failed returns to idle pool
		metricDNSLookup           = "client.dns.lookup"                   // Timer
		metricDNSLookupError      = "client.dns.lookup-error"             // Meter
		metricTCPConnect          = "client.tcp.connect"                  // Timer; +network (tcp/tcp4/tcp6)
		metricTCPConnectError     = "client.tcp.connect-error"            // Meter; +network
		metricTLSHandshakeAttempt = "tls.handshake.attempt"               // Meter
		metricTLSHandshakeFailure = "tls.handshake.failure"               // Meter; +cipher, next_protocol, tls_version
		metricTLSHandshake        = "tls.handshake"                       // Meter; +cipher, next_protocol, tls_version
		metricTimeToFirstByte     = "client.time-to-first-byte"           // Timer; WroteRequest → GotFirstResponseByte
		metricRequestWriteError   = "client.request.write-error"          // Meter
	)
	// ClientTrace callbacks run sequentially on the request's owning goroutine,
	// so these closure-shared locals need no synchronization.
	var (
		getConnStart   time.Time
		dnsStart       time.Time
		connectStarts  = map[string]time.Time{} // network+addr keyed for Happy Eyeballs
		wroteRequestAt time.Time
	)
	return &httptrace.ClientTrace{
		GetConn: func(hostPort string) {
			getConnStart = time.Now()
		},
		GotConn: func(info httptrace.GotConnInfo) {
			reuseTag := metricTagConnectionNew
			if info.Reused {
				reuseTag = metricTagConnectionReused
			}
			registry.Counter(metricConnCreate, reuseTag).Inc(1)
			if !getConnStart.IsZero() {
				registry.Timer(metricConnAcquire, reuseTag).UpdateSince(getConnStart)
			}
		},
		PutIdleConn: func(err error) {
			if err != nil {
				registry.Meter(metricConnIdleReturnError).Mark(1)
				logger.Warn("Idle connection return error", svc1log.Stacktrace(err))
			}
		},
		GotFirstResponseByte: func() {
			if !wroteRequestAt.IsZero() {
				registry.Timer(metricTimeToFirstByte).UpdateSince(wroteRequestAt)
			}
		},
		DNSStart: func(info httptrace.DNSStartInfo) {
			dnsStart = time.Now()
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			if !dnsStart.IsZero() {
				registry.Timer(metricDNSLookup).UpdateSince(dnsStart)
			}
			if info.Err != nil {
				registry.Meter(metricDNSLookupError).Mark(1)
				logger.Warn("DNS Lookup error", svc1log.Stacktrace(info.Err))
			}
		},
		// Happy Eyeballs may produce multiple ConnectStart/ConnectDone pairs, so we key start times by network+addr.
		ConnectStart: func(network, addr string) {
			connectStarts[network+addr] = time.Now()
		},
		ConnectDone: func(network, addr string, err error) {
			networkTag := metrics.NewTagWithFallbackValue(metricTagNetwork, network, "unknown")
			if start, ok := connectStarts[network+addr]; ok {
				registry.Timer(metricTCPConnect, networkTag).UpdateSince(start)
				delete(connectStarts, network+addr)
			}
			if err != nil {
				registry.Meter(metricTCPConnectError, networkTag).Mark(1)
				logger.Warn("TCP Connect error", svc1log.Stacktrace(err))
			}
		},
		TLSHandshakeStart: func() {
			registry.Meter(metricTLSHandshakeAttempt).Mark(1)
		},
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			tags := []metrics.Tag{
				metrics.NewTagWithFallbackValue(metricTagCipher, tls.CipherSuiteName(state.CipherSuite), "unknown"),
				metrics.NewTagWithFallbackValue(metricTagNextProtocol, state.NegotiatedProtocol, "unknown"),
				metrics.NewTagWithFallbackValue(metricTagTLSVersion, tlsVersionString(state.Version), "unknown"),
			}
			if err != nil {
				registry.Meter(metricTLSHandshakeFailure, tags...).Mark(1)
				logger.Warn("TLS Handshake error", svc1log.Stacktrace(err))
			} else {
				registry.Meter(metricTLSHandshake, tags...).Mark(1)
			}
		},
		// Record the latest WroteRequest so TTFB measures from the successful write
		// (the callback can fire multiple times on retried requests).
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			wroteRequestAt = time.Now()
			if info.Err != nil {
				registry.Meter(metricRequestWriteError).Mark(1)
				logger.Warn("Request Write error", svc1log.Stacktrace(info.Err))
			}
		},
	}
}

func responseMetricTags(providers []TagsProvider, req *http.Request, resp *http.Response, err error) metrics.Tags {
	tags := []metrics.Tag{
		tagStatusFamily(resp, err),
		metrics.NewTagWithFallbackValue(metricTagMethod, req.Method, "unknown"),
		tagMethodName(req),
	}
	for _, tp := range providers {
		tags = append(tags, tp.Tags(req, resp, err)...)
	}
	return tags
}

func tagMethodName(req *http.Request) metrics.Tag {
	if name, ok := RPCMethodName(req.Context()); ok && name != "" {
		return metrics.NewTagWithFallbackValue(metricTagMethodName, name, "RPCMethodNameInvalid")
	}
	return metrics.MustNewTag(metricTagMethodName, "RPCMethodNameMissing")
}

func tagStatusFamily(resp *http.Response, err error) metrics.Tag {
	switch {
	case isTimeoutError(err):
		return metricTagFamilyTimeout
	case resp == nil, resp.StatusCode < 100:
		return metricTagFamilyOther
	case resp.StatusCode < 200:
		return metricTagFamily1xx
	case resp.StatusCode < 300:
		return metricTagFamily2xx
	case resp.StatusCode < 400:
		return metricTagFamily3xx
	case resp.StatusCode < 500:
		return metricTagFamily4xx
	case resp.StatusCode < 600:
		return metricTagFamily5xx
	default:
		return metricTagFamilyOther
	}
}

func tlsVersionString(version uint16) string {
	switch version {
	case tls.VersionTLS10:
		return "TLS10"
	case tls.VersionTLS11:
		return "TLS11"
	case tls.VersionTLS12:
		return "TLS12"
	case tls.VersionTLS13:
		return "TLS13"
	}
	return ""
}

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	rootErr := werror.RootCause(err)
	if rootErr == nil {
		return false
	}
	if nerr, ok := rootErr.(net.Error); ok && nerr.Timeout() {
		return true
	}
	if errors.Is(rootErr, context.Canceled) || errors.Is(rootErr, context.DeadlineExceeded) {
		return true
	}
	// N.B. the http package does not expose these error types
	if rootErr.Error() == "net/http: request canceled" || rootErr.Error() == "net/http: request canceled while waiting for connection" {
		return true
	}
	return false
}
