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

package httpc_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/palantir/pkg/refreshable/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validBaseURL = "https://example.invalid"

// TestBuilder_SetHTTPProxyURL_Validation verifies the direct setter rejects the
// same proxy URLs ApplyConfig does, defers the error to Build, and that a later
// valid value (or clear) replaces the prior error.
func TestBuilder_SetHTTPProxyURL_Validation(t *testing.T) {
	ctx := context.Background()

	_, err := httpc.NewBuilder().SetBaseURLs(validBaseURL).SetHTTPProxyURL("://bad").Build(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid HTTP proxy URL")

	_, err = httpc.NewBuilder().SetBaseURLs(validBaseURL).SetHTTPProxyURL("ftp://proxy.example.com").Build(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported scheme")

	// A valid scheme builds; both http and https are accepted.
	_, err = httpc.NewBuilder().SetBaseURLs(validBaseURL).SetHTTPProxyURL("https://proxy.example.com:8080").Build(ctx)
	require.NoError(t, err)

	// Re-setting the field with a valid value clears the prior deferred error.
	_, err = httpc.NewBuilder().
		SetBaseURLs(validBaseURL).
		SetHTTPProxyURL("ftp://proxy.example.com").
		SetHTTPProxyURL("http://proxy.example.com").
		Build(ctx)
	require.NoError(t, err)
}

// TestBuilder_SetSocksProxyURL_Validation verifies SOCKS scheme enforcement and
// replaceable errors.
func TestBuilder_SetSocksProxyURL_Validation(t *testing.T) {
	ctx := context.Background()

	_, err := httpc.NewBuilder().SetBaseURLs(validBaseURL).SetSocksProxyURL("http://proxy.example.com").Build(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid SOCKS proxy URL")
	assert.Contains(t, err.Error(), "unsupported scheme")

	for _, valid := range []string{"socks5://proxy.example.com:1080", "socks5h://proxy.example.com:1080"} {
		_, err = httpc.NewBuilder().SetBaseURLs(validBaseURL).SetSocksProxyURL(valid).Build(ctx)
		require.NoError(t, err, "scheme %q should be accepted", valid)
	}

	// "" clears the field and any prior error.
	_, err = httpc.NewBuilder().
		SetBaseURLs(validBaseURL).
		SetSocksProxyURL("http://proxy.example.com").
		SetSocksProxyURL("").
		Build(ctx)
	require.NoError(t, err)
}

// TestBuilder_SetNoProxy_ClearsProxyErrors verifies SetNoProxy drops deferred
// HTTP and SOCKS proxy validation errors.
func TestBuilder_SetNoProxy_ClearsProxyErrors(t *testing.T) {
	_, err := httpc.NewBuilder().
		SetBaseURLs(validBaseURL).
		SetHTTPProxyURL("ftp://proxy.example.com").
		SetSocksProxyURL("http://proxy.example.com").
		SetNoProxy().
		Build(context.Background())
	require.NoError(t, err)
}

// TestBuilder_SetBaseURLs_Validation verifies direct base-URL validation parity
// with ApplyConfig, including the strict treatment of empty strings, and that a
// later valid value clears the error.
func TestBuilder_SetBaseURLs_Validation(t *testing.T) {
	ctx := context.Background()

	_, err := httpc.NewBuilder().SetBaseURLs("://bad").Build(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid base URL")

	// Strict: an empty string is invalid (unlike ApplyConfig, which drops it).
	_, err = httpc.NewBuilder().SetBaseURLs("https://ok.example.com", "").Build(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid base URL")

	_, err = httpc.NewBuilder().SetBaseURLs("://bad").SetBaseURLs(validBaseURL).Build(ctx)
	require.NoError(t, err)
}

// TestBuilder_DeferredErrors_JoinIndependentFields verifies that errors on
// distinct fields are all reported at Build.
func TestBuilder_DeferredErrors_JoinIndependentFields(t *testing.T) {
	_, err := httpc.NewBuilder().
		SetHTTPProxyURL("ftp://proxy.example.com").
		SetBaseURLs("://bad").
		Build(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid HTTP proxy URL")
	assert.Contains(t, err.Error(), "invalid base URL")
}

// TestBuilder_SetMaxAttempts_Validation_Replaceable verifies the negative-value
// error is field-scoped and replaced by a later valid value.
func TestBuilder_SetMaxAttempts_Validation_Replaceable(t *testing.T) {
	ctx := context.Background()

	_, err := httpc.NewBuilder().SetBaseURLs(validBaseURL).SetMaxAttempts(new(-1)).Build(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SetMaxAttempts")

	_, err = httpc.NewBuilder().SetBaseURLs(validBaseURL).SetMaxAttempts(new(-1)).SetMaxAttempts(new(3)).Build(ctx)
	require.NoError(t, err)
}

// TestBuilder_SetBaseURLsRefreshable_InitialInvalidFailsBuild verifies an
// invalid initial refreshable value fails Build.
func TestBuilder_SetBaseURLsRefreshable_InitialInvalidFailsBuild(t *testing.T) {
	_, err := httpc.NewBuilder().
		SetBaseURLsRefreshable(refreshable.New([]string{"://bad"})).
		Build(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid base URL")
}

// TestBuilder_SetBaseURLsRefreshable_InvalidRefreshRetainsLastValid verifies a
// later invalid refresh is ignored: the live client keeps the last valid URI
// list rather than being poisoned, matching ApplyConfigRefreshable.
func TestBuilder_SetBaseURLsRefreshable_InvalidRefreshRetainsLastValid(t *testing.T) {
	var server1Hits, server2Hits int
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server1Hits++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server1.Close()
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server2Hits++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server2.Close()

	uris := refreshable.New([]string{server1.URL})
	client, err := httpc.NewBuilder().
		SetBaseURLsRefreshable(uris).
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Test", "/test").
		WithDecoder(httpc.VoidDecoder())

	_, _, err = ep.Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, 1, server1Hits)

	// An invalid refresh is ignored; the client keeps using server1.
	uris.Update([]string{"://bad"})
	_, _, err = ep.Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, 2, server1Hits)
	assert.Equal(t, 0, server2Hits)

	// A subsequent valid refresh takes effect.
	uris.Update([]string{server2.URL})
	_, _, err = ep.Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, 2, server1Hits)
	assert.Equal(t, 1, server2Hits)
}
