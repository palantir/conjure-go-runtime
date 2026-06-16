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
)

// Void is the Req or Resp type for endpoints with no request or response body.
type Void = struct{}

// TokenProvider returns a bearer token for request authentication.
type TokenProvider func(ctx context.Context) (string, error)

// BasicAuthProvider returns basic auth credentials for request authentication.
type BasicAuthProvider func(ctx context.Context) (BasicAuth, error)

// BasicAuthOptionalProvider returns basic auth credentials or nil to skip
// setting the Authorization header for this request.
type BasicAuthOptionalProvider func(ctx context.Context) (*BasicAuth, error)
