// Copyright (c) 2018 Palantir Technologies. All rights reserved.
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
	"context"
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient/internal/refreshingclient"
)

// TokenProvider accepts a context and returns either:
//
// (1) a nonempty token and a nil error, or
//
// (2) an empty string and a non-nil error.
//
// A good implementation will request and cache an ephemeral client token.
type TokenProvider func(context.Context) (string, error)

type authTokenMiddleware struct {
	provideToken TokenProvider
}

// RoundTrip wraps an existing round tripper with a token providing round tripper.
// It sets the Authorization header using a newly provided token for each request.
func (h *authTokenMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	token, err := h.provideToken(req.Context())
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}
	return next.RoundTrip(req)
}

// BasicAuthProvider accepts a context and returns either:
//
// (1) a nonempty BasicAuth and a nil error, or
//
// (2) an empty BasicAuth and a non-nil error.
type BasicAuthProvider func(context.Context) (BasicAuth, error)

// BasicAuthOptionalProvider accepts a context and returns either:
//
// (1) nil, nil to indicate that no BasicAuth should not be set on the request, or
//
// (2) a nonempty BasicAuth and a nil error, or
//
// (3) a nil BasicAuth and a non-nil error.
type BasicAuthOptionalProvider func(context.Context) (*BasicAuth, error)

type basicAuthMiddleware struct {
	provideBasicAuth BasicAuthOptionalProvider
}

func (b basicAuthMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	basicAuth, err := b.provideBasicAuth(req.Context())
	if err != nil {
		return nil, err
	}
	if basicAuth != nil {
		setBasicAuth(req.Header, basicAuth.User, basicAuth.Password)
	}
	return next.RoundTrip(req)
}

type refreshableConfigAuthHeaderMiddleware struct {
	cfg refreshingclient.RefreshableValidatedClientParams
}

// newRefreshableConfigAuthHeaderMiddleware returns a new Middleware that sets the Authorization header using the
// current API token or BasicAuth credentials from the provided RefreshableValidatedClientParams. If the request already
// has an Authorization header (e.g. set by a different Middleware), it will not be overwritten.
func newRefreshableConfigAuthHeaderMiddleware(cfg refreshingclient.RefreshableValidatedClientParams) Middleware {
	return &refreshableConfigAuthHeaderMiddleware{cfg: cfg}
}

func (r *refreshableConfigAuthHeaderMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	curr := r.cfg.CurrentValidatedClientParams()
	if curr.APIToken != nil {
		req.Header.Set("Authorization", "Bearer "+*curr.APIToken)
	} else if curr.BasicAuth != nil {
		setBasicAuth(req.Header, curr.BasicAuth.User, curr.BasicAuth.Password)
	}
	return next.RoundTrip(req)
}

func setBasicAuth(h http.Header, username, password string) {
	basicAuthBytes := []byte(username + ":" + password)
	h.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString(basicAuthBytes))
}
