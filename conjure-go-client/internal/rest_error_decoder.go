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
	wSafeParams := werror.SafeParams(safeParams)
	wUnsafeParams := werror.UnsafeParams(unsafeParams)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return werror.Wrap(err, "server returned an error and failed to read body", wSafeParams, wUnsafeParams)
	}
	if len(body) == 0 {
		return werror.Error(resp.Status, wSafeParams, wUnsafeParams)
	}

	isJSON := strings.Contains(resp.Header.Get("Content-Type"), contentTypeJSON)
	if isJSON && d.decodeJSONError != nil {
		if conjureErr, ok := d.decodeJSONError(body); ok {
			return werror.Wrap(conjureErr, "", wSafeParams, wUnsafeParams)
		}
	}
	return werror.Error(resp.Status, wSafeParams, wUnsafeParams, werror.UnsafeParam("responseBody", string(body)))
}
