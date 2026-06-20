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

// Tests added to close coverage gaps identified by the round-2 code review.

package httpc_test

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/palantir/conjure-go-runtime/v3/conjure-go-contract/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// T1: WithBody on an endpoint without an encoder must error rather than
// silently dropping the body.
func TestEndpointExecute_WithBody_NoEncoder_Errors(t *testing.T) {
	ep := httpc.NewPOST[testPayload, struct{}]("NoEncoder", "/test").
		WithDecoder(httpc.VoidDecoder())

	client := clientFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("client should not be called when body has no encoder")
		return nil, nil
	})
	_, _, err := ep.WithBody(testPayload{Name: "x", Value: 1}).Execute(context.Background(), client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no encoder")
}

// T2: Unterminated '{' in the path template must error rather than sending
// the literal brace.
func TestEndpointExecute_UnterminatedPathParam_Errors(t *testing.T) {
	ep := httpc.NewGET[struct{}]("Bad", "/items/{itemId").
		WithDecoder(httpc.VoidDecoder())

	client := clientFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("client should not be called for malformed path")
		return nil, nil
	})
	_, _, err := ep.Execute(context.Background(), client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unterminated")
}

// T3: SetTLSConfig escape hatch is not mutated by SetInsecureSkipVerify.
func TestSetInsecureSkipVerify_DoesNotMutateEscapeHatch(t *testing.T) {
	user := &tls.Config{InsecureSkipVerify: false, MinVersion: tls.VersionTLS13}
	b := httpc.NewBuilder().SetTLSConfig(user).SetInsecureSkipVerify(true)

	result, err := b.BuildTLSConfig(context.Background())
	require.NoError(t, err)
	cfg, validErr := result.Validation()
	require.NoError(t, validErr)
	// The escape-hatch config takes priority — InsecureSkipVerify stays false.
	assert.False(t, cfg.InsecureSkipVerify, "escape-hatch config should not be flipped by SetInsecureSkipVerify")
	assert.Equal(t, uint16(tls.VersionTLS13), cfg.MinVersion)
	// The caller's original config is also untouched (defensive — SetTLSConfig clones).
	assert.False(t, user.InsecureSkipVerify, "caller's config must not be mutated")
}

// T4: Overrides.WithBasicAuth wins over an Overrides.WithHeader("Authorization", ...).
func TestEndpointExecute_BasicAuthOverridesAuthorizationHeader(t *testing.T) {
	var seen string
	transport := &roundTripFunc{fn: func(req *http.Request) (*http.Response, error) {
		seen = req.Header.Get("Authorization")
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody, Request: req}, nil
	}}
	client, err := httpc.NewBuilder().SetBaseURLs("https://example.com").SetTransport(transport).Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Auth", "/auth").
		WithDecoder(httpc.VoidDecoder()).
		WithHeader("Authorization", "Bearer ignored").
		WithBasicAuth("u", "p")

	_, _, err = ep.Execute(context.Background(), client)
	require.NoError(t, err)
	// Basic auth is the trailing Authorization contributor, so it wins.
	assert.Equal(t, "Basic dTpw", seen)
}

// T5: Overrides.WithErrorDecoder(NoErrorDecoder()) bypasses error decoding.
func TestOverrides_NoErrorDecoder_BypassesDefault(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	})

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Err", "/err").
		WithDecoder(httpc.VoidDecoder()).
		WithOverrides(httpc.Overrides{}.WithErrorDecoder(httpc.NoErrorDecoder()))

	client := &httpTestClient{server: server}
	_, httpResp, err := ep.Execute(context.Background(), client)
	require.NoError(t, err)
	require.NotNil(t, httpResp)
	assert.Equal(t, http.StatusForbidden, httpResp.StatusCode)
}

// T6: Overrides.WithConjureErrorDecoder is equivalent to wrapping with
// DefaultErrorDecoderWithConjure.
func TestOverrides_WithConjureErrorDecoder(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errorCode":"INVALID_ARGUMENT","errorName":"Conjure:InvalidArgument","errorInstanceId":"00000000-0000-0000-0000-000000000000","parameters":{}}`))
	})

	var called bool
	ced := &probeConjureDecoder{onDecode: func(name string, body []byte) {
		called = true
		assert.NotEmpty(t, body)
	}}

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Err", "/err").
		WithDecoder(httpc.VoidDecoder()).
		WithOverrides(httpc.Overrides{}.WithConjureErrorDecoder(ced))

	client := &httpTestClient{server: server}
	_, _, err := ep.Execute(context.Background(), client)
	require.Error(t, err)
	assert.True(t, called, "ConjureErrorDecoder should have been invoked")
}

type probeConjureDecoder struct {
	onDecode func(name string, body []byte)
}

func (p *probeConjureDecoder) DecodeConjureError(name string, body []byte) (errors.Error, error) {
	p.onDecode(name, body)
	return errors.NewInvalidArgument(), nil
}

// T7: SetTransport short-circuits the dialer/TLS construction path even when
// SetDialer is also set.
func TestBuilder_SetTransport_WinsOverSetDialer(t *testing.T) {
	var transportCalled bool
	transport := &roundTripFunc{fn: func(req *http.Request) (*http.Response, error) {
		transportCalled = true
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody, Request: req}, nil
	}}
	customDialer := &net.Dialer{Timeout: 99 * time.Hour}
	client, err := httpc.NewBuilder().
		SetBaseURLs("https://example.com").
		SetDialer(customDialer).
		SetTransport(transport).
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "T", "/test").WithDecoder(httpc.VoidDecoder())
	_, _, err = ep.Execute(context.Background(), client)
	require.NoError(t, err)
	assert.True(t, transportCalled, "the SetTransport-provided transport should be used")
}

// T8: Param0/Param1/Param2/ParamVarArgs each forward to the wrapped builder method.
func TestParam_Helpers(t *testing.T) {
	b := httpc.NewBuilder()

	b.Apply(
		httpc.Param0((*httpc.Builder).DisableHTTP2),
		httpc.Param1((*httpc.Builder).SetServiceName, "svc"),
		httpc.Param2((*httpc.Builder).SetBasicAuth, "u", "p"),
		httpc.ParamVarArgs((*httpc.Builder).SetBaseURLs, []string{"https://a", "https://b"}),
	)

	client, err := b.Build(context.Background())
	require.NoError(t, err)
	require.NotNil(t, client)
}

// T9: BasicAuthOptionalProvider returning nil leaves the Authorization header unset.
func TestBuilder_BasicAuthOptionalProvider_NilSkipsAuth(t *testing.T) {
	var seen string
	transport := &roundTripFunc{fn: func(req *http.Request) (*http.Response, error) {
		seen = req.Header.Get("Authorization")
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody, Request: req}, nil
	}}
	client, err := httpc.NewBuilder().
		SetBaseURLs("https://example.com").
		SetTransport(transport).
		SetBasicAuthOptionalProvider(func(context.Context) (*httpc.BasicAuth, error) { return nil, nil }).
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "T", "/t").WithDecoder(httpc.VoidDecoder())
	_, _, err = ep.Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Empty(t, seen, "nil from BasicAuthOptionalProvider should leave Authorization unset")
}

// T10: SetMaxAttempts with a negative value defers a validation error to Build.
func TestBuilder_SetMaxAttempts_NegativeErrors(t *testing.T) {
	neg := -1
	_, err := httpc.NewBuilder().SetBaseURLs("https://example.com").SetMaxAttempts(&neg).Build(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SetMaxAttempts")
}
