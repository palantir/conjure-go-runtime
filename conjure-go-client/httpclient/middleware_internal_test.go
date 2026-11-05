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

package httpclient

import (
	"net/http"
	"strings"
	"testing"

	werror "github.com/palantir/witchcraft-go-error"
	wparams "github.com/palantir/witchcraft-go-params"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMiddlewareFuncTypedNilInMiddlewareChain(t *testing.T) {
	body := &closeTrackingReader{Reader: strings.NewReader("body")}
	ctx := wparams.ContextWithSafeParam(t.Context(), "requestID", "request-id")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com", body)
	require.NoError(t, err)

	var calls []string
	base := middlewareRoundTripperFunc(func(*http.Request) (*http.Response, error) {
		calls = append(calls, "base")
		return &http.Response{StatusCode: http.StatusOK}, nil
	})
	inner := MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		calls = append(calls, "inner")
		return next.RoundTrip(req)
	})
	var typedNil MiddlewareFunc
	outer := MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		calls = append(calls, "outer")
		return next.RoundTrip(req)
	})

	resp, err := wrapTransport(base, inner, typedNil, outer).RoundTrip(req)
	require.Error(t, err)
	assert.ErrorContains(t, err, "nil MiddlewareFunc")
	assert.Nil(t, resp)
	assert.Equal(t, []string{"outer"}, calls)
	assert.Equal(t, 1, body.closes)
	requestID, safe := werror.ParamFromError(err, "requestID")
	assert.Equal(t, "request-id", requestID)
	assert.True(t, safe)
}

func TestMiddlewareFuncNilReceiverIsNotRejected(t *testing.T) {
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)

	base := middlewareRoundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK}, nil
	})
	var middleware *nilReceiverMiddleware

	resp, err := wrapTransport(base, middleware).RoundTrip(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

type middlewareRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f middlewareRoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type nilReceiverMiddleware struct{}

func (*nilReceiverMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	return next.RoundTrip(req)
}
