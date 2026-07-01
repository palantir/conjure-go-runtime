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

package oauth2auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc/oauth2auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestTokenSource(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  oauth2.TokenSource
		want string
	}{
		{
			name: "bearer default when TokenType empty",
			src:  oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "abc"}),
			want: "Bearer abc",
		},
		{
			name: "custom token type passes through verbatim",
			src:  oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "abc", TokenType: "Custom"}),
			want: "Custom abc",
		},
		{
			name: "empty access token leaves Authorization unset",
			src:  oauth2.StaticTokenSource(&oauth2.Token{}),
			want: "",
		},
		{
			name: "nil token leaves Authorization unset",
			src:  oauth2.StaticTokenSource(nil),
			want: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := oauth2auth.TokenSource(tc.src).AuthorizationHeader(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestTokenSource_TokenError(t *testing.T) {
	sentinel := errors.New("boom")
	_, err := oauth2auth.TokenSource(errTokenSource{sentinel}).AuthorizationHeader(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
}

func TestTokenSource_NilSource(t *testing.T) {
	// A nil TokenSource resolves to an error rather than panicking.
	_, err := oauth2auth.TokenSource(nil).AuthorizationHeader(context.Background())
	require.Error(t, err)
}

type errTokenSource struct{ err error }

func (e errTokenSource) Token() (*oauth2.Token, error) { return nil, e.err }
