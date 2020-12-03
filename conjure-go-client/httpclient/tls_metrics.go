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
	"net/http"
	"net/http/httptrace"

	"github.com/palantir/pkg/metrics"
)

const (
	MetricTLSHandshakeAttempt = "tls.handshake.attempt.count"
	MetricTLSHandshakeFailure = "tls.handshake.failure.count"
	MetricTLSHandshake        = "tls.handshake.count"
	CipherTagKey              = "cipher"
	NextProtocolTagKey        = "next_protocol"
	TLSVersionTagKey          = "tls_version"
)

// TLSMetricsMiddleware produces metrics on TLS handshake counts and attempts.
type tlsMetricsMiddleware struct {
}

// RoundTrip will emit meter metrics for TLS handshake attempts, TLS handshake failures, and successful TLS handshakes.
// Successful handshakes will be tagged with details of the TLS connection as well.
func (t *tlsMetricsMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	clientTraceContext := httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
		TLSHandshakeStart: func() {
			metrics.FromContext(req.Context()).Meter(MetricTLSHandshakeAttempt).Mark(1)
		},
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			var tags []metrics.Tag
			cipherSuite := tls.CipherSuiteName(state.CipherSuite)
			if cipherSuite != "" {
				tags = append(tags, metrics.MustNewTag(CipherTagKey, cipherSuite))
			}
			if state.NegotiatedProtocol != "" {
				tags = append(tags, metrics.MustNewTag(NextProtocolTagKey, state.NegotiatedProtocol))
			}
			if tlsVersion := tlsVersionString(state.Version); tlsVersion != "" {
				tags = append(tags, metrics.MustNewTag(TLSVersionTagKey, tlsVersion))
			}
			if err != nil {
				metrics.FromContext(req.Context()).Meter(MetricTLSHandshakeFailure, tags...).Mark(1)
			} else {
				metrics.FromContext(req.Context()).Meter(MetricTLSHandshake, tags...).Mark(1)
			}
		},
	})
	resp, err := next.RoundTrip(req.WithContext(clientTraceContext))
	return resp, err
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
