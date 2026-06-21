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
	"reflect"
	"testing"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCall_DescriptorReuseIsIndependent verifies a single descriptor (the common
// package-level var) yields independent Calls: per-call configuration on one Call
// does not leak into another, nor mutate the descriptor.
func TestCall_DescriptorReuseIsIndependent(t *testing.T) {
	var got string
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Tenant")
		w.WriteHeader(http.StatusNoContent)
	})
	client := &httpTestClient{server: server}

	// Reusable descriptor, configured once.
	ep := httpc.NewGET[struct{}]("Get", "/x").WithDecoder(httpc.VoidDecoder())

	_, _, err := ep.Call().WithHeader("X-Tenant", "acme").Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, "acme", got)

	// A second Call from the same descriptor does not see the first Call's header.
	_, _, err = ep.Call().Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Empty(t, got, "the descriptor is unchanged; a fresh Call carries no per-call header")
}

// TestCall_WithPathParam fills the descriptor's path template per call.
func TestCall_WithPathParam(t *testing.T) {
	var gotPath string
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	client := &httpTestClient{server: server}

	ep := httpc.NewGET[struct{}]("Get", "/items/{id}").WithDecoder(httpc.VoidDecoder())
	_, _, err := ep.Call().WithPathParam("id", "widget").Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, "/items/widget", gotPath)

	// The template is intact on the descriptor: a second call fills it independently.
	_, _, err = ep.Call().WithPathParam("id", "gadget").Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, "/items/gadget", gotPath)
}

// TestCall_PerCallOverridesDescriptorDefault verifies a Call seeds from the
// descriptor's static defaults, and a per-call value (direct or via WithOverrides)
// wins over the descriptor default.
func TestCall_PerCallOverridesDescriptorDefault(t *testing.T) {
	var got string
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Env")
		w.WriteHeader(http.StatusNoContent)
	})
	client := &httpTestClient{server: server}

	// Descriptor bakes a static default header.
	ep := httpc.NewGET[struct{}]("Get", "/x").WithDecoder(httpc.VoidDecoder()).WithHeader("X-Env", "default")

	t.Run("default applies when the call does not override", func(t *testing.T) {
		_, _, err := ep.Call().Execute(context.Background(), client)
		require.NoError(t, err)
		assert.Equal(t, "default", got)
	})
	t.Run("per-call WithHeader wins", func(t *testing.T) {
		_, _, err := ep.Call().WithHeader("X-Env", "prod").Execute(context.Background(), client)
		require.NoError(t, err)
		assert.Equal(t, "prod", got)
	})
	t.Run("per-call WithOverrides wins", func(t *testing.T) {
		_, _, err := ep.Call().WithOverrides(httpc.Overrides{}.WithHeader("X-Env", "staging")).Execute(context.Background(), client)
		require.NoError(t, err)
		assert.Equal(t, "staging", got)
	})
}

// TestCall_NoBodyAndBody covers the two descriptor kinds end to end: a no-body
// Call() and a body Call(body) that encodes through the descriptor's encoder.
func TestCall_NoBodyAndBody(t *testing.T) {
	t.Run("no body", func(t *testing.T) {
		server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodDelete, r.Method)
			w.WriteHeader(http.StatusNoContent)
		})
		ep := httpc.NewDELETE[struct{}]("Del", "/x").WithDecoder(httpc.VoidDecoder())
		_, _, err := ep.Call().Execute(context.Background(), &httpTestClient{server: server})
		require.NoError(t, err)
	})
	t.Run("body encodes via the descriptor encoder", func(t *testing.T) {
		server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"ok","value":1}`))
		})
		ep := httpc.NewPOST[testPayload, testPayload]("Create", "/x").WithJSON()
		out, _, err := ep.Call(testPayload{Name: "in", Value: 9}).Execute(context.Background(), &httpTestClient{server: server})
		require.NoError(t, err)
		assert.Equal(t, testPayload{Name: "ok", Value: 1}, out)
	})
}

// TestCall_BodyPresenceIsTypeEnforced documents structurally that body presence
// is enforced by the type split: the descriptors expose no Execute (it lives only
// on Call), and a BodyEndpoint's Call requires the body argument. The whole test
// suite compiling against this API is the positive proof; this reflection guard
// pins the invariant so a stray Execute on a descriptor is caught here too.
func TestCall_BodyPresenceIsTypeEnforced(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeOf(httpc.BodyEndpoint[testPayload, testPayload]{}),
		reflect.TypeOf(httpc.NoBodyEndpoint[testPayload]{}),
	} {
		_, hasExecute := typ.MethodByName("Execute")
		assert.False(t, hasExecute, "%s must not expose Execute; it lives on Call", typ)
	}
	_, callHasExecute := reflect.TypeOf(httpc.Call[testPayload]{}).MethodByName("Execute")
	assert.True(t, callHasExecute, "Call must expose Execute")
}
