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
	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOverrides_Clone_Independence(t *testing.T) {
	original := httpc.Overrides{}.
		WithAddedHeader("X-A", "1").
		WithAddedQuery("q", "v").
		WithTimeout(5 * time.Second)

	clone := original.Clone()

	// Modify the clone.
	clone = clone.WithAddedHeader("X-B", "2")
	clone = clone.WithAddedQuery("q2", "v2")

	// Verify original is unaffected by verifying through an endpoint execution.
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "1", r.Header.Get("X-A"))
		assert.Empty(t, r.Header.Get("X-B"), "original should not have X-B")
		assert.Equal(t, "v", r.URL.Query().Get("q"))
		assert.Empty(t, r.URL.Query().Get("q2"), "original should not have q2")
		w.WriteHeader(http.StatusNoContent)
	})

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Test", "/test").
		WithDecoder(httpc.VoidDecoder()).
		WithOverrides(original)

	client := &httpTestClient{server: server}
	_, _, err := ep.Execute(context.Background(), client)
	require.NoError(t, err)
}

func TestOverrides_CopyOnWrite(t *testing.T) {
	base := httpc.Overrides{}.WithAddedHeader("X-Base", "base")
	derived := base.WithAddedHeader("X-Derived", "derived")

	// Verify base does not have derived header.
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "base", r.Header.Get("X-Base"))
		assert.Empty(t, r.Header.Get("X-Derived"), "base should not have X-Derived")
		w.WriteHeader(http.StatusNoContent)
	})

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Test", "/test").
		WithDecoder(httpc.VoidDecoder()).
		WithOverrides(base)

	client := &httpTestClient{server: server}
	_, _, err := ep.Execute(context.Background(), client)
	require.NoError(t, err)

	// Verify derived has both headers.
	server2 := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "base", r.Header.Get("X-Base"))
		assert.Equal(t, "derived", r.Header.Get("X-Derived"))
		w.WriteHeader(http.StatusNoContent)
	})

	ep2 := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Test", "/test").
		WithDecoder(httpc.VoidDecoder()).
		WithOverrides(derived)

	client2 := &httpTestClient{server: server2}
	_, _, err = ep2.Execute(context.Background(), client2)
	require.NoError(t, err)
}

func TestOverrides_WithOverrides_Merge(t *testing.T) {
	t.Run("headers additive", func(t *testing.T) {
		server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "v1", r.Header.Get("X-A"))
			assert.Equal(t, "v2", r.Header.Get("X-B"))
			w.WriteHeader(http.StatusNoContent)
		})

		base := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Test", "/test").
			WithDecoder(httpc.VoidDecoder()).
			WithAddedHeader("X-A", "v1")

		overrides := httpc.Overrides{}.WithAddedHeader("X-B", "v2")
		merged := base.WithOverrides(overrides)

		client := &httpTestClient{server: server}
		_, _, err := merged.Execute(context.Background(), client)
		require.NoError(t, err)
	})

	t.Run("query params additive", func(t *testing.T) {
		server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "a", r.URL.Query().Get("p1"))
			assert.Equal(t, "b", r.URL.Query().Get("p2"))
			w.WriteHeader(http.StatusNoContent)
		})

		base := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Test", "/test").
			WithDecoder(httpc.VoidDecoder()).
			WithAddedQuery("p1", "a")

		overrides := httpc.Overrides{}.WithAddedQuery("p2", "b")
		merged := base.WithOverrides(overrides)

		client := &httpTestClient{server: server}
		_, _, err := merged.Execute(context.Background(), client)
		require.NoError(t, err)
	})

	t.Run("timeout last wins", func(t *testing.T) {
		// Use a very short timeout from overrides to verify it takes precedence.
		server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond)
			w.WriteHeader(http.StatusNoContent)
		})

		base := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Test", "/test").
			WithDecoder(httpc.VoidDecoder()).
			WithTimeout(10 * time.Second)

		overrides := httpc.Overrides{}.WithTimeout(50 * time.Millisecond)
		merged := base.WithOverrides(overrides)

		client := &httpTestClient{server: server}
		_, _, err := merged.Execute(context.Background(), client)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context deadline exceeded")
	})

	t.Run("basic auth last wins", func(t *testing.T) {
		server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			user, pass, ok := r.BasicAuth()
			assert.True(t, ok)
			assert.Equal(t, "override-user", user)
			assert.Equal(t, "override-pass", pass)
			w.WriteHeader(http.StatusNoContent)
		})

		base := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Test", "/test").
			WithDecoder(httpc.VoidDecoder()).
			WithBasicAuth("original-user", "original-pass")

		overrides := httpc.Overrides{}.WithBasicAuth("override-user", "override-pass")
		merged := base.WithOverrides(overrides)

		client := &httpTestClient{server: server}
		_, _, err := merged.Execute(context.Background(), client)
		require.NoError(t, err)
	})

	t.Run("middlewares appended", func(t *testing.T) {
		var order []string
		server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})

		mw1 := httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
			order = append(order, "mw1")
			return next.RoundTrip(req)
		})
		mw2 := httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
			order = append(order, "mw2")
			return next.RoundTrip(req)
		})

		base := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Test", "/test").
			WithDecoder(httpc.VoidDecoder()).
			WithMiddleware(mw1)

		overrides := httpc.Overrides{}.WithMiddleware(mw2)
		merged := base.WithOverrides(overrides)

		client := &httpTestClient{server: server}
		_, _, err := merged.Execute(context.Background(), client)
		require.NoError(t, err)
		// Middlewares wrap from outside in: last added (mw2) wraps mw1,
		// so mw2 executes first.
		assert.Equal(t, []string{"mw2", "mw1"}, order)
	})

	t.Run("error decoder last wins", func(t *testing.T) {
		server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		})

		originalDecoder := &testErrorDecoder{}
		overrideDecoder := &countingErrorDecoder{}

		base := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Test", "/test").
			WithDecoder(httpc.VoidDecoder()).
			WithErrorDecoder(originalDecoder)

		overrides := httpc.Overrides{}.WithErrorDecoder(overrideDecoder)
		merged := base.WithOverrides(overrides)

		client := &httpTestClient{server: server}
		_, _, err := merged.Execute(context.Background(), client)
		require.Error(t, err)
		assert.Equal(t, 1, overrideDecoder.called, "override decoder should have been called")
	})
}

func TestOverrides_EmptyMergeIsIdentity(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "v1", r.Header.Get("X-Custom"))
		assert.Equal(t, "bar", r.URL.Query().Get("foo"))
		user, pass, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "user", user)
		assert.Equal(t, "pass", pass)
		w.WriteHeader(http.StatusNoContent)
	})

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Test", "/test").
		WithDecoder(httpc.VoidDecoder()).
		WithAddedHeader("X-Custom", "v1").
		WithAddedQuery("foo", "bar").
		WithBasicAuth("user", "pass")

	// Merge empty overrides — should produce identical behavior.
	merged := ep.WithOverrides(httpc.Overrides{})

	client := &httpTestClient{server: server}
	_, _, err := merged.Execute(context.Background(), client)
	require.NoError(t, err)
}

// countingErrorDecoder counts how many times it's called.
type countingErrorDecoder struct {
	called int
}

func (d *countingErrorDecoder) Handles(resp *http.Response) bool {
	return resp.StatusCode >= 400
}

func (d *countingErrorDecoder) DecodeError(resp *http.Response) error {
	d.called++
	return &testError{statusCode: resp.StatusCode, body: "override"}
}

func TestOverrides_WithHeader(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, []string{"only-value"}, r.Header.Values("X-Single"))
		w.WriteHeader(http.StatusNoContent)
	})

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Test", "/test").
		WithDecoder(httpc.VoidDecoder()).
		WithHeader("X-Single", "only-value")

	client := &httpTestClient{server: server}
	_, _, err := ep.Execute(context.Background(), client)
	require.NoError(t, err)
}

func TestOverrides_WithQuery(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, []string{"final"}, r.URL.Query()["key"])
		w.WriteHeader(http.StatusNoContent)
	})

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Test", "/test").
		WithDecoder(httpc.VoidDecoder()).
		WithQuery("key", "final")

	client := &httpTestClient{server: server}
	_, _, err := ep.Execute(context.Background(), client)
	require.NoError(t, err)
}

func TestOverrides_WithHeaderClearsAdded(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		// WithHeader after WithAddedHeader for the same key should only produce the Set value.
		assert.Equal(t, []string{"2"}, r.Header.Values("X-Key"))
		w.WriteHeader(http.StatusNoContent)
	})

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Test", "/test").
		WithDecoder(httpc.VoidDecoder()).
		WithAddedHeader("X-Key", "1").
		WithHeader("X-Key", "2")

	client := &httpTestClient{server: server}
	_, _, err := ep.Execute(context.Background(), client)
	require.NoError(t, err)
}

func TestOverrides_WithQueryClearsAdded(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, []string{"final"}, r.URL.Query()["q"])
		w.WriteHeader(http.StatusNoContent)
	})

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Test", "/test").
		WithDecoder(httpc.VoidDecoder()).
		WithAddedQuery("q", "first").
		WithQuery("q", "final")

	client := &httpTestClient{server: server}
	_, _, err := ep.Execute(context.Background(), client)
	require.NoError(t, err)
}

func TestOverrides_Merge_WithHeaderClearsAdded(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Merging WithHeader from overrides should replace receiver's WithAddedHeader.
		assert.Equal(t, []string{"replaced"}, r.Header.Values("X-Key"))
		w.WriteHeader(http.StatusNoContent)
	})

	base := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Test", "/test").
		WithDecoder(httpc.VoidDecoder()).
		WithAddedHeader("X-Key", "original")

	overrides := httpc.Overrides{}.WithHeader("X-Key", "replaced")
	merged := base.WithOverrides(overrides)

	client := &httpTestClient{server: server}
	_, _, err := merged.Execute(context.Background(), client)
	require.NoError(t, err)
}
