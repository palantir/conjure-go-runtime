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
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc/internal"
	"github.com/palantir/conjure-go-runtime/v3/conjure-go-contract/errors"
	werror "github.com/palantir/witchcraft-go-error"
)

// ErrorDecoder reports whether a response represents an error and, if so,
// converts it to a Go error.
type ErrorDecoder interface {
	// Handles returns true if DecodeError should be called on resp.
	Handles(resp *http.Response) bool
	DecodeError(resp *http.Response) error
}

// StatusCodeFromError returns the 'statusCode' werror parameter, or ok=false
// if the error has none. [DefaultErrorDecoder] sets this parameter; custom
// decoders may not.
func StatusCodeFromError(err error) (statusCode int, ok bool) {
	return internal.StatusCodeFromError(err)
}

// LocationFromError returns the 'location' werror parameter, or ok=false if
// the error has none. [DefaultErrorDecoder] sets this on 3xx responses that
// carry a Location header.
func LocationFromError(err error) (location string, ok bool) {
	return internal.LocationFromError(err)
}

// unwrapURLError converts a *url.Error to a werror, preserving any werror
// params already on the underlying error.
func unwrapURLError(ctx context.Context, respErr error) error {
	if respErr == nil {
		return nil
	}
	urlErr, ok := respErr.(*url.Error)
	if !ok {
		return respErr
	}
	params := []werror.Param{werror.SafeParam("requestMethod", urlErr.Op)}
	if parsedURL, _ := url.Parse(urlErr.URL); parsedURL != nil {
		params = append(params,
			werror.SafeParam("requestHost", parsedURL.Host),
			werror.UnsafeParam("requestPath", parsedURL.Path))
	}
	return werror.WrapWithContextParams(ctx, urlErr.Err, "httpc request failed", params...)
}

// DefaultErrorDecoder handles responses with status >= 307. For JSON responses
// it tries to unmarshal a Conjure error using the default registry; otherwise it
// includes the raw body as an unsafe parameter. 3xx responses also carry the
// Location header. Use [StatusCodeFromError] to read back the status. This is the
// fallback used by [Call.Execute] when neither the descriptor, call, nor
// [Overrides] provides one; to opt out, use WithNoErrorDecoder on the descriptor,
// call, or overrides.
//
// To decode custom Conjure error types, use [DefaultErrorDecoderWithConjure] or
// [WithConjureErrorDecoder] with your own [errors.ConjureErrorDecoder].
func DefaultErrorDecoder() ErrorDecoder {
	return defaultErrorDecoder{}
}

// DefaultErrorDecoderWithConjure is like [DefaultErrorDecoder] but unmarshals typed
// Conjure errors with the provided [errors.ConjureErrorDecoder] instead of the default
// registry.
func DefaultErrorDecoderWithConjure(ced errors.ConjureErrorDecoder) ErrorDecoder {
	return defaultErrorDecoder{ced: ced}
}

// WithConjureErrorDecoder sets a Conjure error decoder on any [RequestOverrides] value
// (a descriptor, [Call], or [Overrides]):
//
//	ep = httpc.WithConjureErrorDecoder(ep, ced)
//
// It is sugar for d.WithErrorDecoder(DefaultErrorDecoderWithConjure(ced)).
func WithConjureErrorDecoder[D RequestOverrides[D]](d D, ced errors.ConjureErrorDecoder) D {
	return d.WithErrorDecoder(DefaultErrorDecoderWithConjure(ced))
}

// WithConjureErrorParameterFormat sets the Accept-Conjure-Error-Parameter-Format header on
// any [RequestOverrides] value, asking Conjure servers to serialize error parameters in
// format. It is a best-effort hint; decoding stays tolerant of both forms.
func WithConjureErrorParameterFormat[D RequestOverrides[D]](d D, format errors.ConjureErrorParameterFormat) D {
	return d.WithHeader(errors.AcceptConjureErrorParameterFormatHeader, string(format))
}

// NoErrorDecoder returns an [ErrorDecoder] that handles no responses. Use it via
// WithNoErrorDecoder or WithErrorDecoder on a descriptor, [Call], or [Overrides]
// to bypass [DefaultErrorDecoder] and have [Call.Execute] return the raw response
// for every status code.
func NoErrorDecoder() ErrorDecoder {
	return noErrorDecoder{}
}

type noErrorDecoder struct{}

func (noErrorDecoder) Handles(*http.Response) bool { return false }
func (noErrorDecoder) DecodeError(*http.Response) error {
	return nil // unreachable; Handles always returns false.
}

// maxErrorBodyBytes caps how much of an error response body this decoder reads and
// retains as the unsafe 'responseBody' param. A hostile or buggy server can stream
// unbounded bytes within the per-attempt timeout (which bounds time, not memory), and
// many in-flight requests amplify it, so the read is capped. 1 MiB is far larger than any
// legitimate Conjure error body.
const maxErrorBodyBytes = 1 << 20

// StatusError is the error [DefaultErrorDecoder] returns for a response with status
// >= 307. It carries the status code and (for 3xx) the Location, and — when the body
// decoded to a Conjure error — wraps that error so errors.As and
// [errors.GetConjureError] still reach it. Read the status via [StatusCodeFromError]
// or errors.As to a *StatusError.
type StatusError struct {
	Resp *http.Response

	cause     error // decoded Conjure error, or nil
	body      []byte
	truncated bool
}

func (e StatusError) Error() string {
	if e.cause != nil {
		return e.cause.Error()
	}
	return e.Resp.Status
}

// Unwrap exposes the decoded Conjure error (if any) to errors.As / errors.Is.
func (e StatusError) Unwrap() error { return e.cause }

// Cause exposes the decoded Conjure error (if any) to werror and
// [errors.GetConjureError], which walk the werror.Causer chain rather than Unwrap.
func (e StatusError) Cause() error { return e.cause }

func (e StatusError) StatusCode() int {
	return e.Resp.StatusCode
}

func (e StatusError) Location() (*url.URL, error) {
	return e.Resp.Location()
}

func (e StatusError) SafeParams() map[string]any {
	out := map[string]any{
		"statusCode": e.StatusCode(),
	}
	if e.truncated {
		out["responseBodyTruncated"] = true
		out["responseBodyLimitBytes"] = maxErrorBodyBytes
	}
	return out
}

func (e StatusError) UnsafeParams() map[string]any {
	out := map[string]any{}
	// Location is meaningful only for a 3xx relocation.
	if e.Resp.StatusCode < http.StatusBadRequest {
		if location, err := e.Location(); err == nil && location != nil {
			out["location"] = location.String()
		}
	}
	// Surface the raw body only when non-empty and there is no structured Conjure error to carry it.
	if e.cause == nil && len(e.body) > 0 {
		out["responseBody"] = string(e.body)
	}
	return out
}

type defaultErrorDecoder struct {
	ced errors.ConjureErrorDecoder
}

func (defaultErrorDecoder) Handles(resp *http.Response) bool {
	return resp.StatusCode >= http.StatusTemporaryRedirect
}

func (d defaultErrorDecoder) DecodeError(resp *http.Response) error {
	// Read one byte past the cap so an over-cap body is detectable, then truncate. The
	// rest of resp.Body is left unread (Close discards it) rather than draining gigabytes.
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes+1))
	truncated := readErr != nil
	if len(body) > maxErrorBodyBytes {
		body = body[:maxErrorBodyBytes]
		truncated = true
	}

	se := StatusError{Resp: resp, body: body, truncated: truncated}

	// A truncated body cannot be valid JSON, so skip typed decoding and surface the raw body.
	if !truncated && strings.Contains(resp.Header.Get("Content-Type"), ContentTypeJSON) {
		if d.ced != nil {
			if cerr, _ := errors.UnmarshalErrorWithDecoder(d.ced, body); cerr != nil {
				se.cause = cerr
			}
		} else {
			if cerr, _ := errors.UnmarshalError(body); cerr != nil {
				se.cause = cerr
			}
		}
	}
	return se
}
