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
)

// metricsMiddleware emits client.response timer metrics.
type metricsMiddleware struct {
	disabled    refreshable.Refreshable[bool]
	serviceName refreshable.Refreshable[string]
	tags        []TagsProvider
}

const (
	// metricClientResponse is a Timer (microseconds) measuring the total round-trip duration of each HTTP request.
	// Tags: service_name, family (1xx-5xx/timeout/other), method, method_name.
	metricClientResponse = "client_response"
	// metricRequestInFlight is a Counter tracking the number of HTTP requests currently in progress.
	// Tags: service_name.
	metricRequestInFlight = "client_request_in_flight"
	// metricConnCreate is a Counter incremented each time a connection is obtained for a request.
	// Tags: service_name, reused (true/false).
	metricConnCreate = "client_connection_create"
	// metricConnAcquire is a Timer (microseconds) measuring the time from requesting a connection (GetConn)
	// to obtaining one (GotConn). This includes pool wait time, and for new connections: DNS, TCP dial, and TLS.
	// Tags: service_name, reused (true/false).
	metricConnAcquire = "client_conn_acquire"
	// metricConnIdleReturnError is a Meter counting connections that failed to return to the idle pool.
	// A spike indicates connection pool saturation or broken connections.
	// Tags: service_name.
	metricConnIdleReturnError = "client_conn_idle_return_error"

	// metricDNSLookup is a Timer (microseconds) measuring DNS resolution duration.
	// Tags: service_name.
	metricDNSLookup = "client_dns_lookup"
	// metricDNSLookupError is a Meter counting DNS resolution failures.
	// Tags: service_name.
	metricDNSLookupError = "client_dns_lookup_error"

	// metricTCPConnect is a Timer (microseconds) measuring TCP dial duration (ConnectStart to ConnectDone).
	// Tags: service_name, network (e.g. "tcp", "tcp4", "tcp6").
	metricTCPConnect = "client_tcp_connect"
	// metricTCPConnectError is a Meter counting TCP connection failures.
	// Tags: service_name, network.
	metricTCPConnectError = "client_tcp_connect_error"

	// metricTLSHandshakeAttempt is a Meter counting TLS handshake attempts.
	// Tags: service_name.
	metricTLSHandshakeAttempt = "tls_handshake_attempt"
	// metricTLSHandshakeFailure is a Meter counting TLS handshake failures.
	// Tags: service_name, cipher, next_protocol, tls_version (when available).
	metricTLSHandshakeFailure = "tls_handshake_failure"
	// metricTLSHandshake is a Meter counting successful TLS handshakes.
	// Tags: service_name, cipher, next_protocol, tls_version.
	metricTLSHandshake = "tls_handshake"

	// metricTimeToFirstByte is a Timer (microseconds) measuring the interval from request fully written (WroteRequest)
	// to the first response byte received (GotFirstResponseByte). Approximates server-side processing time.
	// Tags: service_name.
	metricTimeToFirstByte = "client_time_to_first_byte"
	// metricRequestWriteError is a Meter counting failures when writing the request to the connection.
	// Tags: service_name.
	metricRequestWriteError = "client_request_write_error"

	metricTagServiceName = "service_name"
	metricTagFamily      = "family"
	metricTagMethod      = "method"
	metricTagMethodName  = "method_name"
	metricTagNetwork     = "network"

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

func (m *metricsMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	if m.disabled != nil && m.disabled.Current() {
		return next.RoundTrip(req)
	}
	serviceNameTag := metrics.NewTagWithFallbackValue(metricTagServiceName, m.serviceName.Current(), "unknown")
	registry := metrics.FromContext(req.Context())

	registry.Counter(metricRequestInFlight, serviceNameTag).Inc(1)
	start := time.Now()
	tlsCtx := m.tlsTraceContext(req.Context(), registry, serviceNameTag)
	resp, err := next.RoundTrip(req.WithContext(tlsCtx))
	duration := time.Since(start)
	registry.Counter(metricRequestInFlight, serviceNameTag).Dec(1)

	tags := m.appendTags([]metrics.Tag{serviceNameTag}, req, resp, err)
	registry.Timer(metricClientResponse, tags...).Update(duration / time.Microsecond)
	return resp, err
}

func (m *metricsMiddleware) appendTags(tags metrics.Tags, req *http.Request, resp *http.Response, err error) metrics.Tags {
	// status family
	tags = append(tags, tagStatusFamily(resp, err)...)
	// method
	tags = append(tags, metrics.MustNewTag(metricTagMethod, req.Method))
	// RPC method name
	if name, ok := RPCMethodName(req.Context()); ok && name != "" {
		if tag, tagErr := metrics.NewTag(metricTagMethodName, name); tagErr == nil {
			tags = append(tags, tag)
		} else {
			tags = append(tags, metrics.MustNewTag(metricTagMethodName, "RPCMethodNameInvalid"))
		}
	} else {
		tags = append(tags, metrics.MustNewTag(metricTagMethodName, "RPCMethodNameMissing"))
	}
	// custom tags
	for _, tp := range m.tags {
		for k, v := range tp.Tags(req, resp, err) {
			if tag, tagErr := metrics.NewTag(k, v); tagErr == nil {
				tags = append(tags, tag)
			}
		}
	}
	return tags
}

func (m *metricsMiddleware) tlsTraceContext(ctx context.Context, registry metrics.Registry, serviceNameTag metrics.Tag) context.Context {
	// Local timing variables shared across closures. ClientTrace callbacks are invoked
	// sequentially on the goroutine that owns the request, so no synchronization is needed.
	var (
		getConnStart   time.Time
		dnsStart       time.Time
		connectStarts  = map[string]time.Time{} // keyed by network+addr for Happy Eyeballs
		wroteRequestAt time.Time
	)
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GetConn: func(hostPort string) {
			getConnStart = time.Now()
		},
		GotConn: func(info httptrace.GotConnInfo) {
			reuseTag := metricTagConnectionNew
			if info.Reused {
				reuseTag = metricTagConnectionReused
			}
			registry.Counter(metricConnCreate, serviceNameTag, reuseTag).Inc(1)
			if !getConnStart.IsZero() {
				registry.Timer(metricConnAcquire, serviceNameTag, reuseTag).Update(time.Since(getConnStart) / time.Microsecond)
			}
		},
		PutIdleConn: func(err error) {
			if err != nil {
				registry.Meter(metricConnIdleReturnError, serviceNameTag).Mark(1)
			}
		},
		GotFirstResponseByte: func() {
			if !wroteRequestAt.IsZero() {
				registry.Timer(metricTimeToFirstByte, serviceNameTag).Update(time.Since(wroteRequestAt) / time.Microsecond)
			}
		},
		DNSStart: func(info httptrace.DNSStartInfo) {
			dnsStart = time.Now()
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			if !dnsStart.IsZero() {
				registry.Timer(metricDNSLookup, serviceNameTag).Update(time.Since(dnsStart) / time.Microsecond)
			}
			if info.Err != nil {
				registry.Meter(metricDNSLookupError, serviceNameTag).Mark(1)
			}
		},
		// ConnectStart/ConnectDone may be called multiple times with Happy Eyeballs
		// (dual-stack IPv4/IPv6), so we key start times by network+addr.
		ConnectStart: func(network, addr string) {
			connectStarts[network+addr] = time.Now()
		},
		ConnectDone: func(network, addr string, err error) {
			networkTag := metrics.NewTagWithFallbackValue(metricTagNetwork, network, "unknown")
			if start, ok := connectStarts[network+addr]; ok {
				registry.Timer(metricTCPConnect, serviceNameTag, networkTag).Update(time.Since(start) / time.Microsecond)
				delete(connectStarts, network+addr)
			}
			if err != nil {
				registry.Meter(metricTCPConnectError, serviceNameTag, networkTag).Mark(1)
			}
		},
		TLSHandshakeStart: func() {
			registry.Meter(metricTLSHandshakeAttempt, serviceNameTag).Mark(1)
		},
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			tags := []metrics.Tag{serviceNameTag}
			cipherSuite := tls.CipherSuiteName(state.CipherSuite)
			if cipherSuite != "" {
				tags = append(tags, metrics.NewTagWithFallbackValue(metricTagCipher, cipherSuite, "unknown"))
			}
			if state.NegotiatedProtocol != "" {
				tags = append(tags, metrics.NewTagWithFallbackValue(metricTagNextProtocol, state.NegotiatedProtocol, "unknown"))
			}
			if tlsVersion := tlsVersionString(state.Version); tlsVersion != "" {
				tags = append(tags, metrics.NewTagWithFallbackValue(metricTagTLSVersion, tlsVersion, "unknown"))
			}
			if err != nil {
				registry.Meter(metricTLSHandshakeFailure, tags...).Mark(1)
			} else {
				registry.Meter(metricTLSHandshake, tags...).Mark(1)
			}
		},
		// WroteRequest may be called multiple times for retried requests. We always
		// record the latest time so that TTFB measures from the successful write.
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			wroteRequestAt = time.Now()
			if info.Err != nil {
				registry.Meter(metricRequestWriteError, serviceNameTag).Mark(1)
			}
		},
	})
}

func tagStatusFamily(resp *http.Response, err error) metrics.Tags {
	switch {
	case isTimeoutError(err):
		return metrics.Tags{metricTagFamilyTimeout}
	case resp == nil, resp.StatusCode < 100, resp.StatusCode > 599:
		return metrics.Tags{metricTagFamilyOther}
	case resp.StatusCode < 200:
		return metrics.Tags{metricTagFamily1xx}
	case resp.StatusCode < 300:
		return metrics.Tags{metricTagFamily2xx}
	case resp.StatusCode < 400:
		return metrics.Tags{metricTagFamily3xx}
	case resp.StatusCode < 500:
		return metrics.Tags{metricTagFamily4xx}
	case resp.StatusCode < 600:
		return metrics.Tags{metricTagFamily5xx}
	}
	return metrics.Tags{}
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
