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
	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient/httpc"
)

// A Middleware wraps an http client's request and is able to read or modify the request and response.
type Middleware = httpc.Middleware

// MiddlewareFunc is a convenience type alias that implements Middleware.
type MiddlewareFunc = httpc.MiddlewareFunc
