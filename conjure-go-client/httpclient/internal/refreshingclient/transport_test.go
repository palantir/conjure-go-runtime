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
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"testing"

	"github.com/palantir/pkg/refreshable/v2"
	"github.com/stretchr/testify/require"
)

func TestRefreshableTransportIgnoresInvalidTLSUpdates(t *testing.T) {
	tlsConfig := refreshable.New(&tls.Config{ServerName: "initial"})
	validatedTLSConfig, _, err := refreshable.Validate(t.Context(), tlsConfig, func(_ context.Context, config *tls.Config) error {
		if config.ServerName == "invalid" {
			return errors.New("invalid TLS config")
		}
		return nil
	})
	require.NoError(t, err)

	transport := NewRefreshableTransport(t.Context(), refreshable.New(TransportParams{}), validatedTLSConfig, &net.Dialer{}).(*RefreshableTransport)
	initial := transport.Refreshable.Current()()

	tlsConfig.Update(&tls.Config{ServerName: "invalid"})
	require.Same(t, initial, transport.Refreshable.Current()())

	tlsConfig.Update(&tls.Config{ServerName: "refreshed"})
	require.NotSame(t, initial, transport.Refreshable.Current()())
}

func TestManagedTransportDefersRetiredCleanupUntilRequestsComplete(t *testing.T) {
	state := newManagedTransport(&http.Transport{})
	closeCalls := 0
	state.closeIdleConnections = func() {
		closeCalls++
	}

	require.True(t, state.acquire())
	state.retire()
	require.Equal(t, 0, closeCalls)
	require.False(t, state.acquire())

	state.release()
	require.Equal(t, 1, closeCalls)

	state.retire()
	require.Equal(t, 1, closeCalls)
}
