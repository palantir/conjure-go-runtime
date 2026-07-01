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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/palantir/pkg/bytesbuffers"
	"github.com/palantir/witchcraft-go-logging/wlog/svclog/svc1log"
)

// Call is a single request invocation derived from an endpoint descriptor via
// [BodyEndpoint.Call] or [NoBodyEndpoint.Call]. It carries the per-invocation
// state — the (captured) request body, filled path parameters, and any per-call
// overrides — that an endpoint descriptor intentionally does not. Methods are
// copy-on-write, so a Call may be configured and executed without affecting the
// descriptor it came from.
//
// A Call seeds its configuration from the descriptor's static defaults; per-call
// [RequestOverrides] methods and [Call.WithOverrides] layer on top, winning over
// the descriptor defaults. Invoke [Call.Execute] to send the request.
type Call[Resp any] struct {
	method       string
	path         string // populated from the descriptor's path template; fill with WithPathParam
	name         string
	accept       string
	decoder      BodyDecoder[Resp]
	overrides    Overrides
	encode       func(*http.Request) error // nil for a no-body call
	pathParamErr error                     // deferred from WithPathParam (e.g. traversal); surfaced by Execute
}

// newCall builds a Call from a descriptor core, seeding per-call configuration
// from the descriptor's static defaults. encode is nil for a no-body call.
func newCall[Resp any](core endpointCore[Resp], encode func(*http.Request) error) Call[Resp] {
	return Call[Resp]{
		method:    core.method,
		path:      core.pathTemplate,
		name:      core.name,
		accept:    core.accept,
		decoder:   core.decoder,
		overrides: core.defaults,
		encode:    encode,
	}
}

func errNoEncoder(name string) error {
	return fmt.Errorf("httpc: endpoint %s has a body but no encoder; call WithEncoder (or WithJSON) before Call", name)
}

// WithPathParam fills a named {key} placeholder in the path with
// url.PathEscape(fmt.Sprint(value)). Parameters can be filled in any order:
//
//	getItem.Call().WithPathParam("itemId", id).WithPathParam("version", v).Execute(ctx, client)
//
// A trailing greedy placeholder ({key*}) preserves slashes while still escaping
// each individual segment:
//
//	getFile.Call().WithPathParam("filePath", "dir/sub dir/file.txt")
//	// → /files/dir/sub%20dir/file.txt
//
// A "." or ".." path segment is rejected (deferring an error to Execute) so an
// untrusted value cannot climb the path: for a greedy value any such segment is
// rejected; a non-greedy value is rejected only when the whole value is "." or ".."
// (it cannot span segments, since "/" is escaped to %2F).
func (c Call[Resp]) WithPathParam(key string, value any) Call[Resp] {
	s := fmt.Sprint(value)
	if glob := "{" + key + "*}"; strings.Contains(c.path, glob) {
		segments := strings.Split(s, "/")
		c = c.withPathParamErr(key, segments)
		if c.pathParamErr != nil {
			return c
		}
		for i, seg := range segments {
			segments[i] = url.PathEscape(seg)
		}
		c.path = strings.ReplaceAll(c.path, glob, strings.Join(segments, "/"))
	} else {
		c = c.withPathParamErr(key, []string{s})
		if c.pathParamErr != nil {
			return c
		}
		c.path = strings.ReplaceAll(c.path, "{"+key+"}", url.PathEscape(s))
	}
	return c
}

func (c Call[Resp]) withPathParamErr(key string, segments []string) Call[Resp] {
	for _, seg := range segments {
		if seg == "." || seg == ".." {
			if c.pathParamErr == nil {
				c.pathParamErr = fmt.Errorf("httpc: path parameter %q must not contain a %q segment (possible path traversal)", key, seg)
			}
		}
	}
	return c
}

// WithOverrides merges o into this call's per-request configuration: o's set
// headers/query replace and its adds accumulate; the scalars (timeout, error
// decoder, authorizer, buffer pool) are last-wins (o wins for any it set,
// including an explicit clear); middlewares append.
func (c Call[Resp]) WithOverrides(o Overrides) Call[Resp] {
	c.overrides = c.overrides.merge(o)
	return c
}

func (c Call[Resp]) WithHeader(key, value string, additionalValues ...string) Call[Resp] {
	c.overrides = c.overrides.WithHeader(key, value, additionalValues...)
	return c
}

func (c Call[Resp]) WithAddedHeader(key, value string, additionalValues ...string) Call[Resp] {
	c.overrides = c.overrides.WithAddedHeader(key, value, additionalValues...)
	return c
}

func (c Call[Resp]) WithQuery(key, value string, additionalValues ...string) Call[Resp] {
	c.overrides = c.overrides.WithQuery(key, value, additionalValues...)
	return c
}

func (c Call[Resp]) WithAddedQuery(key, value string, additionalValues ...string) Call[Resp] {
	c.overrides = c.overrides.WithAddedQuery(key, value, additionalValues...)
	return c
}

func (c Call[Resp]) WithAddedQueryValues(q url.Values) Call[Resp] {
	c.overrides = c.overrides.WithAddedQueryValues(q)
	return c
}

func (c Call[Resp]) WithTimeout(d time.Duration) Call[Resp] {
	c.overrides = c.overrides.WithTimeout(d)
	return c
}

func (c Call[Resp]) WithUnlimitedTimeout() Call[Resp] {
	c.overrides = c.overrides.WithUnlimitedTimeout()
	return c
}

func (c Call[Resp]) WithDefaultTimeout() Call[Resp] {
	c.overrides = c.overrides.WithDefaultTimeout()
	return c
}

func (c Call[Resp]) WithErrorDecoder(d ErrorDecoder) Call[Resp] {
	c.overrides = c.overrides.WithErrorDecoder(d)
	return c
}

func (c Call[Resp]) WithNoErrorDecoder() Call[Resp] {
	c.overrides = c.overrides.WithNoErrorDecoder()
	return c
}

func (c Call[Resp]) WithDefaultErrorDecoder() Call[Resp] {
	c.overrides = c.overrides.WithDefaultErrorDecoder()
	return c
}

func (c Call[Resp]) WithAuthorization(a Authorizer) Call[Resp] {
	c.overrides = c.overrides.WithAuthorization(a)
	return c
}

func (c Call[Resp]) WithDefaultAuthorization() Call[Resp] {
	c.overrides = c.overrides.WithDefaultAuthorization()
	return c
}

func (c Call[Resp]) WithMiddleware(m Middleware) Call[Resp] {
	c.overrides = c.overrides.WithMiddleware(m)
	return c
}

func (c Call[Resp]) WithBufferPool(p bytesbuffers.Pool) Call[Resp] {
	c.overrides = c.overrides.WithBufferPool(p)
	return c
}

func (c Call[Resp]) WithDefaultBufferPool() Call[Resp] {
	c.overrides = c.overrides.WithDefaultBufferPool()
	return c
}

// Execute builds an *http.Request from the call, sends it via [Runtime.Send],
// and decodes the response.
//
// The returned *http.Response has its body consumed (drained or handed to the
// decoder). It may be non-nil on error when the server replied but the decoded
// response represents a failure.
func (c Call[Resp]) Execute(ctx context.Context, client Runtime) (Resp, *http.Response, error) {
	var zero Resp

	if c.pathParamErr != nil {
		return zero, nil, c.pathParamErr
	}

	if i := strings.IndexByte(c.path, '{'); i != -1 {
		j := strings.IndexByte(c.path[i:], '}')
		if j == -1 {
			return zero, nil, fmt.Errorf("httpc: unterminated path parameter starting at %q in %s %s", c.path[i:], c.method, c.path)
		}
		param := c.path[i : i+j+1]
		return zero, nil, fmt.Errorf("httpc: path parameter %s not populated in %s %s", param, c.method, c.path)
	}

	if c.name != "" {
		ctx = ContextWithRPCMethodName(ctx, c.name)
	}
	if c.overrides.bufferPool.value != nil {
		ctx = contextWithBufferPool(ctx, c.overrides.bufferPool.value)
	}

	// Path-only request; the runtime prepends the base URI on each attempt.
	req, err := http.NewRequestWithContext(ctx, c.method, c.path, nil)
	if err != nil {
		return zero, nil, err
	}

	if c.encode != nil {
		if err := c.encode(req); err != nil {
			return zero, nil, err
		}
	}
	if c.accept != "" {
		req.Header.Set("Accept", c.accept)
	}

	// The encoder's Content-Type and the Accept header were written straight onto
	// req. Hoist them into a codec-level RequestValues below the per-call overrides,
	// then clear req's headers, so every header resolves through the same per-attempt
	// path: endpoint codec headers sit ABOVE builder-intrinsic headers (a client
	// SetHeader("Accept"/"Content-Type") can't override them) while a per-call
	// WithHeader still wins. Per-request middlewares run innermost (per attempt, with
	// the resolved URL); a per-request timeout overrides the per-attempt bound (a
	// total-call deadline is the caller's job via ctx).
	opts := SendOptions{
		Values:      requestValuesFromHeader(req.Header).concat(c.overrides.requestValues()),
		Middlewares: c.overrides.middlewares,
		Policy:      c.overrides.callPolicyOverrides(),
	}
	req.Header = make(http.Header)

	resp, err := client.Send(ctx, req, opts)
	if err != nil {
		return zero, nil, err
	}

	decoder := c.overrides.errorDecoder.value
	if decoder == nil {
		decoder = DefaultErrorDecoder()
	}
	if decoder.Handles(resp) {
		decodeErr := decoder.DecodeError(resp)
		drainBody(ctx, resp)
		return zero, resp, decodeErr
	}

	if c.decoder != nil {
		result, err := c.decoder.Decode(ctx, resp)
		if err != nil {
			drainBody(ctx, resp)
			return zero, resp, err
		}
		// rawBodyDecoder hands the body to the caller; don't drain.
		if _, raw := c.decoder.(rawBodyDecoder); !raw {
			drainBody(ctx, resp)
		}
		return result, resp, nil
	}
	drainBody(ctx, resp)
	return zero, resp, nil
}

func drainBody(ctx context.Context, resp *http.Response) {
	// drain and close treated as best-effort
	if resp != nil && resp.Body != nil {
		if bytes, err := io.Copy(io.Discard, resp.Body); err != nil {
			svc1log.FromContext(ctx).Warn("Failed to drain entire response body", svc1log.SafeParam("bytes", bytes), svc1log.Stacktrace(err))
		} else if bytes > 0 {
			svc1log.FromContext(ctx).Debug("Drained remaining response body", svc1log.SafeParam("bytes", bytes))
		}
		if err := resp.Body.Close(); err != nil {
			svc1log.FromContext(ctx).Warn("Failed to close response body", svc1log.Stacktrace(err))
		}
	}
}
