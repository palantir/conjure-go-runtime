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

package httpc

import (
	"context"
	"encoding/base64"

	"github.com/palantir/pkg/refreshable/v2"
	werror "github.com/palantir/witchcraft-go-error"
)

// Authorizer produces the Authorization header for a request. It is the single
// auth abstraction across httpc: the builder ([Builder.SetAuth]), per-call
// configuration ([RequestOverrides.WithAuthorization]), and config application
// all resolve to one. Construct one with [BearerToken], [BasicCredentials], a
// provider/refreshable constructor, [NoAuthorization], or [AuthorizerFunc].
//
// Returning the full header value (scheme included) keeps any scheme working —
// e.g. an OAuth2 adapter can emit a non-Bearer token type verbatim.
type Authorizer interface {
	// AuthorizationHeader returns the full Authorization header value (e.g.
	// "Bearer abc123"), or "" to send no Authorization header.
	//
	// It is resolved lazily — only if this authorizer wins Authorization
	// precedence (a higher-precedence request header or per-call authorizer
	// supersedes it, in which case this is never called) — and, under the
	// standard runtime, once per attempt during request decoration. An
	// Authorizer reused across requests (e.g. a builder-level one) must be safe
	// for concurrent calls.
	AuthorizationHeader(ctx context.Context) (string, error)
}

// AuthorizerFunc adapts an ordinary function to an [Authorizer], like
// http.HandlerFunc. A nil AuthorizerFunc resolves to an error rather than
// panicking.
type AuthorizerFunc func(ctx context.Context) (string, error)

// AuthorizationHeader implements [Authorizer].
func (f AuthorizerFunc) AuthorizationHeader(ctx context.Context) (string, error) {
	if f == nil {
		return "", werror.ErrorWithContextParams(ctx, "httpc: nil AuthorizerFunc")
	}
	return f(ctx)
}

// NoAuthorization returns an [Authorizer] that intentionally sends no
// credentials (it yields ""). Unlike clearing auth back to a default (a nil
// authorizer / [RequestOverrides.WithDefaultAuthorization]), it is itself a
// present, winning contributor: at a higher-precedence layer it suppresses
// lower-layer auth for the request rather than falling through to it.
func NoAuthorization() Authorizer {
	return AuthorizerFunc(func(context.Context) (string, error) { return "", nil })
}

// BearerToken returns an [Authorizer] for a static bearer token, sent as
// "Authorization: Bearer <token>". An empty token sends no header.
func BearerToken(token string) Authorizer {
	header := bearerAuthHeader(token)
	return AuthorizerFunc(func(context.Context) (string, error) { return header, nil })
}

// BearerTokenProvider returns an [Authorizer] that calls provider for a bearer
// token per resolution. A good provider requests and caches an ephemeral token.
func BearerTokenProvider(provider func(ctx context.Context) (string, error)) Authorizer {
	if provider == nil {
		return nilProviderAuthorizer("BearerTokenProvider")
	}
	return AuthorizerFunc(func(ctx context.Context) (string, error) {
		token, err := provider(ctx)
		if err != nil {
			return "", err
		}
		return bearerAuthHeader(token), nil
	})
}

// RefreshableBearerToken returns an [Authorizer] backed by a refreshable bearer
// token. A nil current value sends no header.
func RefreshableBearerToken(r refreshable.Refreshable[*string]) Authorizer {
	if r == nil {
		return nilProviderAuthorizer("RefreshableBearerToken")
	}
	return AuthorizerFunc(func(context.Context) (string, error) {
		token := r.Current()
		if token == nil {
			return "", nil
		}
		return bearerAuthHeader(*token), nil
	})
}

// BasicCredentials returns an [Authorizer] for static HTTP basic auth.
func BasicCredentials(user, password string) Authorizer {
	header := basicAuthHeader(user, password)
	return AuthorizerFunc(func(context.Context) (string, error) { return header, nil })
}

// BasicCredentialsProvider returns an [Authorizer] that calls provider for
// basic-auth credentials per resolution. The provider should return a credential
// or an error.
func BasicCredentialsProvider(provider func(ctx context.Context) (BasicAuth, error)) Authorizer {
	if provider == nil {
		return nilProviderAuthorizer("BasicCredentialsProvider")
	}
	return AuthorizerFunc(func(ctx context.Context) (string, error) {
		auth, err := provider(ctx)
		if err != nil {
			return "", err
		}
		return basicAuthHeader(auth.User, auth.Password), nil
	})
}

// OptionalBasicCredentials returns an [Authorizer] whose provider may return nil
// to send no header for a request. A nil result means "send none" — if this
// authorizer wins precedence the Authorization header is left unset; it does not
// fall through to a lower layer.
func OptionalBasicCredentials(provider func(ctx context.Context) (*BasicAuth, error)) Authorizer {
	if provider == nil {
		return nilProviderAuthorizer("OptionalBasicCredentials")
	}
	return AuthorizerFunc(func(ctx context.Context) (string, error) {
		auth, err := provider(ctx)
		if err != nil {
			return "", err
		}
		if auth == nil {
			return "", nil
		}
		return basicAuthHeader(auth.User, auth.Password), nil
	})
}

// RefreshableBasicCredentials returns an [Authorizer] backed by refreshable
// basic-auth credentials. A nil current value sends no header.
func RefreshableBasicCredentials(r refreshable.Refreshable[*BasicAuth]) Authorizer {
	if r == nil {
		return nilProviderAuthorizer("RefreshableBasicCredentials")
	}
	return AuthorizerFunc(func(context.Context) (string, error) {
		auth := r.Current()
		if auth == nil {
			return "", nil
		}
		return basicAuthHeader(auth.User, auth.Password), nil
	})
}

// nilProviderAuthorizer returns an Authorizer that errors at resolution,
// naming the constructor that was handed a nil provider/refreshable. Failing at
// resolution (rather than panicking) keeps a misconfiguration debuggable and off
// the request hot path.
func nilProviderAuthorizer(constructor string) Authorizer {
	return AuthorizerFunc(func(ctx context.Context) (string, error) {
		return "", werror.ErrorWithContextParams(ctx, "httpc: nil provider passed to auth constructor",
			werror.SafeParam("constructor", constructor))
	})
}

func bearerAuthHeader(token string) string {
	if token == "" {
		return ""
	}
	return "Bearer " + token
}

func basicAuthHeader(user, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
}
