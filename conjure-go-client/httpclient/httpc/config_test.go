package httpc_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient"
	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient/httpc"
	"github.com/palantir/pkg/refreshable/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptr[T any](v T) *T { return &v }

func TestApplyConfig_StaticRoundTrip(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	token := "my-secret-token"
	cfg := httpc.ClientConfig{
		ServiceName: "test-svc",
		URIs:        []string{server.URL},
		APIToken:    &token,
	}

	client, err := httpc.NewStandardClientBuilder().
		ApplyConfig(context.Background(), cfg).
		DisableRestErrors().
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "/api/test", "GetTest").
		SetDecoder(httpc.VoidDecoder())
	_, err = ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, "Bearer my-secret-token", gotAuth)
}

func TestApplyConfig_MinimalConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := httpc.ClientConfig{
		URIs: []string{server.URL},
	}

	client, err := httpc.NewStandardClientBuilder().
		ApplyConfig(context.Background(), cfg).
		DisableRestErrors().
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "/test", "Test").
		SetDecoder(httpc.VoidDecoder())
	_, err = ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
}

func TestApplyConfig_AuthPrecedence_APITokenOverBasicAuth(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	token := "bearer-token"
	cfg := httpc.ClientConfig{
		URIs:     []string{server.URL},
		APIToken: &token,
		BasicAuth: &httpclient.BasicAuth{
			User:     "user",
			Password: "pass",
		},
	}

	client, err := httpc.NewStandardClientBuilder().
		ApplyConfig(context.Background(), cfg).
		DisableRestErrors().
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "/test", "Test").
		SetDecoder(httpc.VoidDecoder())
	_, err = ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
	assert.Contains(t, gotAuth, "Bearer bearer-token")
}

func TestApplyConfig_AuthPrecedence_APITokenFileOverBasicAuth(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	require.NoError(t, os.WriteFile(tokenFile, []byte("  file-token  \n"), 0600))

	cfg := httpc.ClientConfig{
		URIs:         []string{server.URL},
		APITokenFile: &tokenFile,
		BasicAuth: &httpclient.BasicAuth{
			User:     "user",
			Password: "pass",
		},
	}

	client, err := httpc.NewStandardClientBuilder().
		ApplyConfig(context.Background(), cfg).
		DisableRestErrors().
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "/test", "Test").
		SetDecoder(httpc.VoidDecoder())
	_, err = ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, "Bearer file-token", gotAuth)
}

func TestApplyConfig_BasicAuth(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := httpc.ClientConfig{
		URIs: []string{server.URL},
		BasicAuth: &httpclient.BasicAuth{
			User:     "user",
			Password: "pass",
		},
	}

	client, err := httpc.NewStandardClientBuilder().
		ApplyConfig(context.Background(), cfg).
		DisableRestErrors().
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "/test", "Test").
		SetDecoder(httpc.VoidDecoder())
	_, err = ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
	assert.Contains(t, gotAuth, "Basic ")
}

func TestApplyConfig_ValidationErrors_InvalidURI(t *testing.T) {
	cfg := httpc.ClientConfig{
		URIs: []string{"://invalid"},
	}

	_, err := httpc.NewStandardClientBuilder().
		ApplyConfig(context.Background(), cfg).
		Build(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid url")
}

func TestApplyConfig_ValidationErrors_InvalidProxyURL(t *testing.T) {
	cfg := httpc.ClientConfig{
		URIs:     []string{"http://localhost"},
		ProxyURL: ptr("://bad-proxy"),
	}

	_, err := httpc.NewStandardClientBuilder().
		ApplyConfig(context.Background(), cfg).
		Build(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid proxy url")
}

func TestApplyConfig_ValidationErrors_UnsupportedProxyScheme(t *testing.T) {
	cfg := httpc.ClientConfig{
		URIs:     []string{"http://localhost"},
		ProxyURL: ptr("ftp://proxy.example.com"),
	}

	_, err := httpc.NewStandardClientBuilder().
		ApplyConfig(context.Background(), cfg).
		Build(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only http(s) and socks5 are supported")
}

func TestApplyConfig_ValidationErrors_MissingTokenFile(t *testing.T) {
	cfg := httpc.ClientConfig{
		URIs:         []string{"http://localhost"},
		APITokenFile: ptr("/nonexistent/token/file"),
	}

	_, err := httpc.NewStandardClientBuilder().
		ApplyConfig(context.Background(), cfg).
		Build(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read api-token-file")
}

func TestApplyConfig_MaxNumRetries(t *testing.T) {
	tests := []struct {
		name         string
		retries      *int
		wantAttempts int
	}{
		{
			name:         "nil retries yields default attempts (2 * len(uris))",
			retries:      nil,
			wantAttempts: 2, // 2 * 1 URI
		},
		{
			name:         "0 retries yields 1 attempt",
			retries:      ptr(0),
			wantAttempts: 1,
		},
		{
			name:         "3 retries yields 4 attempts",
			retries:      ptr(3),
			wantAttempts: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var attempts int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts++
				// Always return 503 to trigger retries.
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer server.Close()

			cfg := httpc.ClientConfig{
				URIs:          []string{server.URL},
				MaxNumRetries: tt.retries,
				// Use short backoff so retries are fast.
				InitialBackoff: ptr(1 * time.Millisecond),
				MaxBackoff:     ptr(1 * time.Millisecond),
			}

			client, err := httpc.NewStandardClientBuilder().
				ApplyConfig(context.Background(), cfg).
				Build(context.Background())
			require.NoError(t, err)

			// Use POST with a body so the request is retryable (GetBody is set).
			ep := httpc.NewEndpoint[builderTestPayload, struct{}](http.MethodPost, "/test", "Test").
				SetEncoder(httpc.JSONEncoder[builderTestPayload]()).
				SetDecoder(httpc.VoidDecoder())
			_, _ = ep.Execute(context.Background(), client, builderTestPayload{Message: "test"})

			assert.Equal(t, tt.wantAttempts, attempts)
		})
	}
}

func TestApplyConfig_EmptyURIsFiltered(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := httpc.ClientConfig{
		URIs: []string{"", server.URL, ""},
	}

	client, err := httpc.NewStandardClientBuilder().
		ApplyConfig(context.Background(), cfg).
		DisableRestErrors().
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "/test", "Test").
		SetDecoder(httpc.VoidDecoder())
	_, err = ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
}

func TestApplyConfig_TimeoutFromConfig(t *testing.T) {
	cfg := httpc.ClientConfig{
		URIs:         []string{"http://localhost"},
		ReadTimeout:  ptr(10 * time.Second),
		WriteTimeout: ptr(20 * time.Second),
	}

	// Build should succeed. The timeout should be max(read, write) = 20s.
	_, err := httpc.NewStandardClientBuilder().
		ApplyConfig(context.Background(), cfg).
		DisableRestErrors().
		Build(context.Background())
	require.NoError(t, err)
}

func TestApplyConfig_MetricsDisabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := httpc.ClientConfig{
		URIs: []string{server.URL},
		Metrics: httpclient.MetricsConfig{
			Enabled: ptr(false),
		},
	}

	client, err := httpc.NewStandardClientBuilder().
		ApplyConfig(context.Background(), cfg).
		DisableRestErrors().
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "/test", "Test").
		SetDecoder(httpc.VoidDecoder())
	_, err = ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
}

func TestApplyConfigRefreshable_BasicRoundTrip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := refreshable.New(httpc.ClientConfig{
		ServiceName: "refreshable-svc",
		URIs:        []string{server.URL},
	})

	client, err := httpc.NewStandardClientBuilder().
		ApplyConfigRefreshable(context.Background(), cfg).
		DisableRestErrors().
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "/test", "Test").
		SetDecoder(httpc.VoidDecoder())
	_, err = ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
}

func TestApplyConfigRefreshable_AuthUpdate(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	token1 := "token-1"
	cfg := refreshable.New(httpc.ClientConfig{
		URIs:     []string{server.URL},
		APIToken: &token1,
	})

	client, err := httpc.NewStandardClientBuilder().
		ApplyConfigRefreshable(context.Background(), cfg).
		DisableRestErrors().
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "/test", "Test").
		SetDecoder(httpc.VoidDecoder())

	// First request should use token-1.
	_, err = ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, "Bearer token-1", gotAuth)

	// Update config to use basic auth instead.
	cfg.Update(httpc.ClientConfig{
		URIs: []string{server.URL},
		BasicAuth: &httpclient.BasicAuth{
			User:     "user",
			Password: "pass",
		},
	})

	// Next request should use basic auth (no bearer token).
	_, err = ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
	assert.Contains(t, gotAuth, "Basic ")
}

func TestApplyConfigRefreshable_InvalidInitialConfig(t *testing.T) {
	cfg := refreshable.New(httpc.ClientConfig{
		URIs: []string{"://invalid"},
	})

	_, err := httpc.NewStandardClientBuilder().
		ApplyConfigRefreshable(context.Background(), cfg).
		Build(context.Background())
	require.Error(t, err)
}

func TestApplyConfigRefreshable_InvalidRefreshRetainsPrevious(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := refreshable.New(httpc.ClientConfig{
		URIs: []string{server.URL},
	})

	client, err := httpc.NewStandardClientBuilder().
		ApplyConfigRefreshable(context.Background(), cfg).
		DisableRestErrors().
		Build(context.Background())
	require.NoError(t, err)

	// Push an invalid config update (invalid URI).
	cfg.Update(httpc.ClientConfig{
		URIs: []string{"://invalid"},
	})

	// The client should still work with the previous valid config.
	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "/test", "Test").
		SetDecoder(httpc.VoidDecoder())
	_, err = ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
}

func TestApplyConfig_ComposesWithExistingSettings(t *testing.T) {
	// ApplyConfig should only override fields the config explicitly sets.
	// A prior SetTimeout call should not be stomped by a config that doesn't
	// specify ReadTimeout/WriteTimeout.
	var gotUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := httpc.ClientConfig{
		URIs: []string{server.URL},
	}

	client, err := httpc.NewStandardClientBuilder().
		SetUserAgent("my-agent").
		SetTimeout(5*time.Second).
		ApplyConfig(context.Background(), cfg).
		DisableRestErrors().
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "/test", "Test").
		SetDecoder(httpc.VoidDecoder())
	_, err = ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)

	// UserAgent (set before ApplyConfig) should still be present since
	// the config doesn't set it.
	assert.Equal(t, "my-agent", gotUserAgent)
}

func TestApplyConfigRefreshable_ComposesWithExistingSettings(t *testing.T) {
	// ApplyConfigRefreshable should overlay config fields on top of existing
	// builder values, not replace them wholesale.
	var gotUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := refreshable.New(httpc.ClientConfig{
		URIs: []string{server.URL},
	})

	client, err := httpc.NewStandardClientBuilder().
		SetUserAgent("my-agent").
		SetTimeout(5*time.Second).
		ApplyConfigRefreshable(context.Background(), cfg).
		DisableRestErrors().
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "/test", "Test").
		SetDecoder(httpc.VoidDecoder())
	_, err = ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)

	assert.Equal(t, "my-agent", gotUserAgent)
}

func TestApplyConfig_ProxyHTTPS(t *testing.T) {
	cfg := httpc.ClientConfig{
		URIs:     []string{"http://localhost"},
		ProxyURL: ptr("https://proxy.example.com:8080"),
	}

	// Should succeed — https is a valid proxy scheme.
	_, err := httpc.NewStandardClientBuilder().
		ApplyConfig(context.Background(), cfg).
		DisableRestErrors().
		Build(context.Background())
	require.NoError(t, err)
}

func TestApplyConfig_ProxySocks5(t *testing.T) {
	cfg := httpc.ClientConfig{
		URIs:     []string{"http://localhost"},
		ProxyURL: ptr("socks5://proxy.example.com:1080"),
	}

	// Should succeed — socks5 is a valid proxy scheme.
	_, err := httpc.NewStandardClientBuilder().
		ApplyConfig(context.Background(), cfg).
		DisableRestErrors().
		Build(context.Background())
	require.NoError(t, err)
}

// TestApplyConfigRefreshable_TimeoutPropagation verifies that updating the timeout
// field in a refreshable config propagates to the *http.Client.
func TestApplyConfigRefreshable_TimeoutPropagation(t *testing.T) {
	cfg := refreshable.New(httpc.ClientConfig{
		URIs:        []string{"http://localhost"},
		ReadTimeout: ptr(5 * time.Second),
	})

	b := httpc.NewStandardClientBuilder().
		ApplyConfigRefreshable(context.Background(), cfg)

	httpClient, err := b.BuildHTTPClient(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, httpClient.Current().Timeout)

	// Update config with a new timeout.
	cfg.Update(httpc.ClientConfig{
		URIs:        []string{"http://localhost"},
		ReadTimeout: ptr(15 * time.Second),
	})
	assert.Equal(t, 15*time.Second, httpClient.Current().Timeout)
}

// TestApplyConfigRefreshable_URIPropagation verifies that updating the URIs
// in a refreshable config causes the built client to route to the new server.
func TestApplyConfigRefreshable_URIPropagation(t *testing.T) {
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

	cfg := refreshable.New(httpc.ClientConfig{
		URIs: []string{server1.URL},
	})

	client, err := httpc.NewStandardClientBuilder().
		ApplyConfigRefreshable(context.Background(), cfg).
		DisableRestErrors().
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "/test", "Test").
		SetDecoder(httpc.VoidDecoder())

	_, err = ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, 1, server1Hits)
	assert.Equal(t, 0, server2Hits)

	// Update config URIs to point to server2.
	cfg.Update(httpc.ClientConfig{
		URIs: []string{server2.URL},
	})

	_, err = ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, 1, server1Hits)
	assert.Equal(t, 1, server2Hits)
}

// TestApplyConfigRefreshable_SetToUnsetFallback verifies that when a refreshable
// config field goes from set to unset, the builder falls back to the pre-config
// value that was captured at ApplyConfigRefreshable call time.
func TestApplyConfigRefreshable_SetToUnsetFallback(t *testing.T) {
	cfg := refreshable.New(httpc.ClientConfig{
		URIs:        []string{"http://localhost"},
		ReadTimeout: ptr(15 * time.Second),
	})

	b := httpc.NewStandardClientBuilder().
		SetTimeout(5 * time.Second). // pre-config timeout
		ApplyConfigRefreshable(context.Background(), cfg)

	httpClient, err := b.BuildHTTPClient(context.Background())
	require.NoError(t, err)

	// Config timeout (15s) should take precedence over pre-config (5s).
	assert.Equal(t, 15*time.Second, httpClient.Current().Timeout)

	// Update config without a timeout — should fall back to pre-config 5s.
	cfg.Update(httpc.ClientConfig{
		URIs: []string{"http://localhost"},
	})
	assert.Equal(t, 5*time.Second, httpClient.Current().Timeout)

	// Re-set config timeout — should override again.
	cfg.Update(httpc.ClientConfig{
		URIs:        []string{"http://localhost"},
		ReadTimeout: ptr(30 * time.Second),
	})
	assert.Equal(t, 30*time.Second, httpClient.Current().Timeout)
}
