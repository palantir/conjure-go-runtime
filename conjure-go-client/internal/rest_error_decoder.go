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

package internal

import (
	"io"
	"net/http"
	"strings"

	werror "github.com/palantir/witchcraft-go-error"
)

const contentTypeJSON = "application/json"

// maxErrorBodyBytes caps how much of an error response body this decoder reads and
// retains as the unsafe 'responseBody' param. A hostile or buggy server can stream
// unbounded bytes within the per-attempt timeout (which bounds time, not memory), and
// many in-flight requests amplify it, so the read is capped. 1 MiB is far larger than any
// legitimate Conjure error body.
const maxErrorBodyBytes = 1 << 20

// RESTErrorDecoder is the shared error decoder behind httpc.DefaultErrorDecoder
// and the conjureerrors decoders. It handles responses with status >= 307,
// tagging errors with the 'statusCode' (and, for 3xx, 'location') params read by
// StatusCodeFromError / LocationFromError. For JSON bodies it delegates to
// decodeJSONError to produce a typed error; a nil decodeJSONError (or one
// returning ok=false) falls back to including the raw body as an unsafe param.
//
// The conjure-specific body parsing is injected via decodeJSONError so this type
// — and httpc — need not depend on conjure-go-contract/errors.
type RESTErrorDecoder struct {
	decodeJSONError func(body []byte) (err error, ok bool)
}

// NewRESTErrorDecoder returns a RESTErrorDecoder using decodeJSONError to parse
// typed errors from JSON response bodies. Pass nil to always fall back to the
// raw body for JSON responses.
func NewRESTErrorDecoder(decodeJSONError func(body []byte) (err error, ok bool)) RESTErrorDecoder {
	return RESTErrorDecoder{decodeJSONError: decodeJSONError}
}

func (RESTErrorDecoder) Handles(resp *http.Response) bool {
	return resp.StatusCode >= http.StatusTemporaryRedirect
}

func (d RESTErrorDecoder) DecodeError(resp *http.Response) error {
	safeParams := map[string]any{
		"statusCode": resp.StatusCode,
	}
	unsafeParams := map[string]any{}
	if resp.StatusCode >= http.StatusTemporaryRedirect &&
		resp.StatusCode < http.StatusBadRequest {
		location, err := resp.Location()
		if err == nil {
			unsafeParams["location"] = location.String()
		}
	}
	wUnsafeParams := werror.UnsafeParams(unsafeParams)

	// Read one byte past the cap so an over-cap body is detectable, then truncate. The
	// rest of resp.Body is left unread (Close discards it) rather than draining gigabytes.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes+1))
	if err != nil {
		return werror.Wrap(err, "server returned an error and failed to read body", werror.SafeParams(safeParams), wUnsafeParams)
	}
	truncated := len(body) > maxErrorBodyBytes
	if truncated {
		body = body[:maxErrorBodyBytes]
		safeParams["responseBodyTruncated"] = true
		safeParams["responseBodyLimitBytes"] = maxErrorBodyBytes
	}
	wSafeParams := werror.SafeParams(safeParams)

	if len(body) == 0 {
		return werror.Error(resp.Status, wSafeParams, wUnsafeParams)
	}

	// A truncated body cannot be valid JSON, so skip typed decoding and surface it as the
	// raw (truncated) responseBody param.
	isJSON := !truncated && strings.Contains(resp.Header.Get("Content-Type"), contentTypeJSON)
	if isJSON && d.decodeJSONError != nil {
		if conjureErr, ok := d.decodeJSONError(body); ok {
			return werror.Wrap(conjureErr, "", wSafeParams, wUnsafeParams)
		}
	}
	return werror.Error(resp.Status, wSafeParams, wUnsafeParams, werror.UnsafeParam("responseBody", string(body)))
}
