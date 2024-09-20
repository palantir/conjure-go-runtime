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
	"errors"
	"fmt"
	"net/http"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient/internal/refreshingclient"
	"github.com/palantir/pkg/refreshable"
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
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	return next.RoundTrip(req)
}

func newAuthTokenMiddlewareFromRefreshable(token refreshable.StringPtr) Middleware {
	return &conditionalMiddleware{
		Disabled: refreshable.NewBool(token.MapStringPtr(func(s *string) interface{} {
			return s == nil
		})),
		Delegate: &authTokenMiddleware{provideToken: func(ctx context.Context) (string, error) {
			if s := token.CurrentStringPtr(); s != nil {
				return *s, nil
			}
			return "", nil
		}},
	}
}

// ErrSkipBasicAuthHeader indicates that a basic auth header should not being added to a request.
// This is useful in cases where a provider for basic auth does not exist, but may exist at some point in the future.
var ErrSkipBasicAuthHeader = errors.New("basic auth headers should not be added")

// BasicAuthProvider accepts a context and returns either:
//
// (1) a nonempty BasicAuth and a nil error, or
//
// (2) an empty BasicAuth and a non-nil error.
//
// (3) an empty BasicAuth and a typed error (ErrSkipBasicAuthHeader) indicating that no basic auth is available.
type BasicAuthProvider func(context.Context) (BasicAuth, error)

// basicAuthMiddleware wraps a refreshing BasicAuth pointer and injects basic auth credentials if the pointer is not nil
type basicAuthMiddleware struct {
	provider BasicAuthProvider
}

func (b *basicAuthMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	basicAuth, err := b.provider(req.Context())
	if err != nil {
		if errors.Is(err, ErrSkipBasicAuthHeader) {
			return next.RoundTrip(req)
		}
		return nil, err
	}
	setBasicAuth(req.Header, basicAuth.User, basicAuth.Password)
	return next.RoundTrip(req)
}

func newBasicAuthMiddlewareFromRefreshable(auth refreshingclient.RefreshableBasicAuthPtr) Middleware {
	return &conditionalMiddleware{
		Disabled: refreshable.NewBool(auth.MapBasicAuthPtr(func(auth *refreshingclient.BasicAuth) interface{} {
			return auth == nil
		})),
		Delegate: &basicAuthMiddleware{provider: func(ctx context.Context) (BasicAuth, error) {
			if b := auth.CurrentBasicAuthPtr(); b != nil {
				return BasicAuth{User: b.User, Password: b.Password}, nil
			}
			return BasicAuth{}, nil
		}},
	}
}

func setBasicAuth(h http.Header, username, password string) {
	basicAuthBytes := []byte(username + ":" + password)
	h.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString(basicAuthBytes))
}
