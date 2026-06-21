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
)

// TokenProvider accepts a context and returns either:
//
// (1) a nonempty token and a nil error, or
//
// (2) an empty string and a non-nil error.
//
// A good implementation will request and cache an ephemeral client token.
type TokenProvider func(context.Context) (string, error)

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

// basicAuthValue returns the Authorization header value for HTTP basic auth with the given
// credentials. Cross-host redirect protection lives in httpc core (the auth contributor is
// gated during request decoration), so the bridge no longer needs its own redirect-aware
// header setter — all bridge auth flows through httpc's single Authorizer path.
func basicAuthValue(username, password string) string {
	basicAuthBytes := []byte(username + ":" + password)
	return "Basic " + base64.StdEncoding.EncodeToString(basicAuthBytes)
}
