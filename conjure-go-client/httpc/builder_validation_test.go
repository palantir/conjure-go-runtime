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

// TestBuilder_SetBaseURLs_StrictServiceOrigin verifies a base URL must be a service
// origin (supported scheme + host, optional base path): non-service forms — path-only,
// hostless, opaque, unsupported scheme, userinfo, query, fragment — are rejected, while
// http/https/mesh schemes with an optional base path are accepted.
func TestBuilder_SetBaseURLs_StrictServiceOrigin(t *testing.T) {
	ctx := context.Background()

	for _, bad := range []string{
		"/foo",                               // path-only
		"http://",                            // hostless
		"http:foo",                           // opaque
		"ftp://host.example.com",             // unsupported scheme
		"https://user:pass@host.example.com", // userinfo footgun
		"https://host.example.com?x=1",       // query
		"https://host.example.com#frag",      // fragment
	} {
		_, err := httpc.NewBuilder().SetBaseURLs(bad).Build(ctx)
		require.Error(t, err, "expected %q to be rejected", bad)
		assert.Contains(t, err.Error(), "invalid base URL", "url %q", bad)
	}

	for _, ok := range []string{
		"https://host.example.com",
		"http://host.example.com:8080",
		"https://host.example.com/base/path",
		"mesh-https://host.example.com",
		"mesh-http://host.example.com/base",
	} {
		_, err := httpc.NewBuilder().SetBaseURLs(ok).Build(ctx)
		require.NoError(t, err, "expected %q to be accepted", ok)
	}
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

	ep := httpc.NewNoBodyEndpoint[struct{}](http.MethodGet, "Test", "/test").
		WithDecoder(httpc.VoidDecoder())

	_, _, err = ep.Call().Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, 1, server1Hits)

	// An invalid refresh is ignored; the client keeps using server1.
	uris.Update([]string{"://bad"})
	_, _, err = ep.Call().Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, 2, server1Hits)
	assert.Equal(t, 0, server2Hits)

	// A subsequent valid refresh takes effect.
	uris.Update([]string{server2.URL})
	_, _, err = ep.Call().Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, 2, server1Hits)
	assert.Equal(t, 1, server2Hits)
}

// TestBuilder_SetBaseURLsRefreshable_RecoversBeforeBuild verifies an initially
// invalid refreshable that becomes valid before Build no longer fails (the
// deferred error is re-evaluated against the live value).
func TestBuilder_SetBaseURLsRefreshable_RecoversBeforeBuild(t *testing.T) {
	ctx := context.Background()
	uris := refreshable.New([]string{"://bad"})
	b := httpc.NewBuilder().SetBaseURLsRefreshable(uris)

	_, err := b.Build(ctx)
	require.Error(t, err, "initial invalid value should fail Build")

	uris.Update([]string{"https://ok.example.com"})
	_, err = b.Build(ctx)
	require.NoError(t, err, "Build should succeed once the refreshable recovers")
}

// TestBuilder_ApplyConfig_BadThenGood verifies a bad config followed by a good
// config recovers (fieldConfig is replaceable, not append-only).
func TestBuilder_ApplyConfig_BadThenGood(t *testing.T) {
	ctx := context.Background()
	b := httpc.NewBuilder().
		ApplyConfig(ctx, httpc.ClientConfig{URIs: []string{"://bad"}}).
		ApplyConfig(ctx, httpc.ClientConfig{URIs: []string{"https://ok.example.com"}})

	_, err := b.Build(ctx)
	require.NoError(t, err, "a later valid ApplyConfig should clear the prior config error")
}

// TestBuilder_ApplyConfigRefreshable_RecoversBeforeBuild verifies a refreshable
// config that starts invalid and becomes valid before Build no longer fails.
func TestBuilder_ApplyConfigRefreshable_RecoversBeforeBuild(t *testing.T) {
	ctx := context.Background()
	cfg := refreshable.New(httpc.ClientConfig{URIs: []string{"://bad"}})
	b := httpc.NewBuilder().ApplyConfigRefreshable(ctx, cfg)

	_, err := b.Build(ctx)
	require.Error(t, err, "initial invalid config should fail Build")

	cfg.Update(httpc.ClientConfig{URIs: []string{"https://ok.example.com"}})
	_, err = b.Build(ctx)
	require.NoError(t, err, "Build should succeed once the config recovers")
}

// TestBuilder_BuildHTTPClient_IgnoresBadBaseURLs verifies the escape-hatch
// *http.Client builder is not blocked by base-URL or max-attempts errors, which
// it does not consume.
func TestBuilder_BuildHTTPClient_IgnoresBadBaseURLs(t *testing.T) {
	ctx := context.Background()

	_, err := httpc.NewBuilder().SetBaseURLs("://bad").BuildHTTPClient(ctx)
	require.NoError(t, err, "BuildHTTPClient should ignore bad base URLs")

	_, err = httpc.NewBuilder().SetMaxAttempts(new(-1)).BuildHTTPClient(ctx)
	require.NoError(t, err, "BuildHTTPClient should ignore bad max attempts")
}

// TestBuilder_BuildTransport_SetTransport_IgnoresProxyError verifies a transport
// override bypasses a stale proxy validation error.
func TestBuilder_BuildTransport_SetTransport_IgnoresProxyError(t *testing.T) {
	rt, err := httpc.NewBuilder().
		SetHTTPProxyURL("ftp://bad-proxy").
		SetTransport(http.DefaultTransport).
		BuildTransport(context.Background())
	require.NoError(t, err, "SetTransport should bypass the deferred HTTP proxy error")
	assert.Equal(t, http.DefaultTransport, rt)
}

// SetMaxAttempts with a negative value defers a validation error to Build.
func TestBuilder_SetMaxAttempts_NegativeErrors(t *testing.T) {
	neg := -1
	_, err := httpc.NewBuilder().SetBaseURLs("https://example.com").SetMaxAttempts(&neg).Build(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SetMaxAttempts")
}
