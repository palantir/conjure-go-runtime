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

// Package oauth2auth adapts a golang.org/x/oauth2.TokenSource to an httpc
// [httpc.Authorizer]. It is intentionally separate from the core httpc package
// so callers who do not use OAuth2 do not pull in golang.org/x/oauth2
// transitively.
package oauth2auth

import (
	"context"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	werror "github.com/palantir/witchcraft-go-error"
	"golang.org/x/oauth2"
)

// TokenSource adapts a [oauth2.TokenSource] to an [httpc.Authorizer], so a token
// minted by any oauth2 flow (client credentials, google.DefaultTokenSource,
// oauth2.ReuseTokenSource, …) can drive httpc auth via SetAuth or WithAuthorization.
//
// The returned Authorizer emits the full header "<Type> <AccessToken>", where Type
// is the token's TokenType or "Bearer" when unset (see [oauth2.Token.Type]), so
// non-Bearer schemes pass through verbatim. A token with an empty AccessToken (or a
// nil token) leaves Authorization unset.
//
// oauth2.TokenSource.Token takes no context, so a context-dependent source must bind
// its context at construction (e.g. via oauth2.NewClient); the per-request context is
// used only to annotate errors. This is a non-issue for the common cached/refreshing
// source, whose Token call returns without I/O.
func TokenSource(ts oauth2.TokenSource) httpc.Authorizer {
	return httpc.AuthorizerFunc(func(ctx context.Context) (string, error) {
		if ts == nil {
			return "", werror.ErrorWithContextParams(ctx, "oauth2auth: nil TokenSource")
		}
		tok, err := ts.Token()
		if err != nil {
			return "", werror.WrapWithContextParams(ctx, err, "oauth2auth: fetching token")
		}
		if tok == nil || tok.AccessToken == "" {
			return "", nil
		}
		return tok.Type() + " " + tok.AccessToken, nil
	})
}
