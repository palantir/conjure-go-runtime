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
)

type ctxKey string

const (
	// context-key for the RPC method name associated with the HTTP request call
	rpcMethodName ctxKey = "rpcMethodName"
	// context-key for the "For-User-Agent" header value
	forUserAgentContextKey ctxKey = "forUserAgent"
)

// ContextWithRPCMethodName returns a copy of ctx with the rpcMethodName key set.
// This enables instrumentation (metrics, tracing, etc) to better identify metrics.
func ContextWithRPCMethodName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, rpcMethodName, name)
}

func getRPCMethodName(ctx context.Context) string {
	e := ctx.Value(rpcMethodName)
	if e == nil {
		return ""
	}
	return e.(string)
}

// ContextWithForUserAgent returns a copy of ctx with the forUserAgent key set to the provided value (unless the
// provided value is the empty string, in which case it returns the provided context).
func ContextWithForUserAgent(ctx context.Context, forUserAgent string) context.Context {
	if forUserAgent == "" {
		return ctx
	}
	return context.WithValue(ctx, forUserAgentContextKey, forUserAgent)
}

func getForUserAgent(ctx context.Context) string {
	e := ctx.Value(forUserAgentContextKey)
	if e == nil {
		return ""
	}
	return e.(string)
}
