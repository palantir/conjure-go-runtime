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
	serviceName string
	tags        []TagsProvider
}

const (
	metricClientResponse  = "client_response"
	metricRequestInFlight = "client_request_in_flight"
	metricConnCreate      = "client_connection_create"

	metricTagServiceName = "service_name"
	metricTagFamily      = "family"
	metricTagMethod      = "method"
	metricTagMethodName  = "method_name"

	metricTLSHandshakeAttempt = "tls_handshake_attempt"
	metricTLSHandshakeFailure = "tls_handshake_failure"
	metricTLSHandshake        = "tls_handshake"

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
	serviceNameTag := metrics.NewTagWithFallbackValue(metricTagServiceName, m.serviceName, "unknown")
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
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			if info.Reused {
				registry.Counter(metricConnCreate, serviceNameTag, metricTagConnectionReused).Inc(1)
			} else {
				registry.Counter(metricConnCreate, serviceNameTag, metricTagConnectionNew).Inc(1)
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
