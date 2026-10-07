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

package refreshingclient

import (
	"crypto/tls"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAllowsTLS13OrLaterAndTLS12(t *testing.T) {
	for _, test := range []struct {
		name     string
		config   *tls.Config
		expected bool
	}{
		{name: "default", config: &tls.Config{MinVersion: tls.VersionTLS12}, expected: true},
		{name: "TLS 1.3 only", config: &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}},
		{name: "TLS 1.2 only", config: &tls.Config{MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12}},
	} {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, allowsTLS13OrLaterAndTLS12(test.config))
		})
	}
}

func TestIsTimeoutError(t *testing.T) {
	assert.False(t, isTimeoutError(nil))
	assert.False(t, isTimeoutError(errors.New("TLS handshake failed")))
	assert.True(t, isTimeoutError(testTimeoutError{}))
}

type testTimeoutError struct{}

func (testTimeoutError) Error() string   { return "timeout" }
func (testTimeoutError) Timeout() bool   { return true }
func (testTimeoutError) Temporary() bool { return true }
