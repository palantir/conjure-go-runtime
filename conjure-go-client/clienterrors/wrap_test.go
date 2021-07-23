// Copyright (c) 2021 Palantir Technologies. All rights reserved.
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

package clienterrors

import (
	"context"
	"net/http"
	"testing"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-contract/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrap(t *testing.T) {
	t.Run("connection refused", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		client := http.DefaultClient
		req, err := http.NewRequest("GET", "http://localhost:12345", nil)
		require.NoError(t, err)
		req.WithContext(ctx)
		resp, err := client.Do(req)
		assert.Nil(t, resp)
		require.Error(t, err)
		newErr := WrapClientError(req, err)
		require.Error(t, err)
		cErr := newErr.(errors.Error)
		require.Equal(t, "HttpClient:ConnectionRefused", cErr.Name())
	})
	t.Run("dns no such host", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		client := http.DefaultClient
		req, err := http.NewRequest("GET", "http://not-a-real-host.foo:12345", nil)
		require.NoError(t, err)
		req.WithContext(ctx)
		resp, err := client.Do(req)
		assert.Nil(t, resp)
		require.Error(t, err)
		newErr := WrapClientError(req, err)
		require.Error(t, err)
		cErr := newErr.(errors.Error)
		require.Equal(t, "HttpClient:DnsNoSuchHost", cErr.Name())
	})
}
