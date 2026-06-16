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
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/url"
	"os"
	"syscall"
	"testing"

	"github.com/palantir/pkg/metrics"
	"github.com/stretchr/testify/assert"
)

// TestTagStatusFamily_ErrorClasses checks transport-error classification, in
// particular the family precedence when an error matches more than one predicate
// (e.g. a DNS timeout matches both isDNSError and isTimeoutError). Errors are
// constructed rather than produced by real network calls so cases stay deterministic.
func TestTagStatusFamily_ErrorClasses(t *testing.T) {
	// wrap models how net/http surfaces transport errors to the caller.
	wrap := func(err error) error { return &url.Error{Op: "Get", URL: "https://example.com", Err: err} }

	for _, tc := range []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "dns not found",
			err:      wrap(&net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "missing.invalid", IsNotFound: true}}),
			expected: "dns_error",
		},
		{
			name:     "dns timeout classified as dns, not timeout",
			err:      wrap(&net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "i/o timeout", Name: "slow.invalid", IsTimeout: true}}),
			expected: "dns_error",
		},
		{
			name:     "tls verification failure",
			err:      wrap(&tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}),
			expected: "tls_verify_error",
		},
		{
			name:     "connection refused",
			err:      wrap(&net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}),
			expected: "connection_error",
		},
		{
			name:     "connection reset",
			err:      wrap(&net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)}),
			expected: "connection_error",
		},
		{
			name:     "host unreachable",
			err:      wrap(&net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.EHOSTUNREACH)}),
			expected: "connection_error",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tags := tagStatusFamily(nil, nil, tc.err)
			expected := metrics.Tags{metrics.MustNewTag(metricTagFamily, tc.expected)}
			assert.Equal(t, expected.ToMap(), tags.ToMap())
		})
	}
}
