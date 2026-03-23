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

package httpclient

import (
	"net/http"
)

// perRequestErrorDecoderKey is the context key for per-request error decoder overrides.
type perRequestErrorDecoderKey struct{}

// combinedErrorDecoder implements ErrorDecoder by checking for a per-request
// decoder override (stored in the request context) before falling back to
// the client-level decoder. This is installed as httpc's error decoder so that
// it runs inside the retry loop, enabling retries on retryable status codes.
type combinedErrorDecoder struct {
	clientDecoder ErrorDecoder
}

func (d *combinedErrorDecoder) Handles(resp *http.Response) bool {
	if resp.Request != nil {
		if reqDecoder, ok := resp.Request.Context().Value(perRequestErrorDecoderKey{}).(ErrorDecoder); ok && reqDecoder != nil {
			if reqDecoder.Handles(resp) {
				return true
			}
		}
	}
	if d.clientDecoder != nil {
		return d.clientDecoder.Handles(resp)
	}
	return false
}

func (d *combinedErrorDecoder) DecodeError(resp *http.Response) error {
	if resp.Request != nil {
		if reqDecoder, ok := resp.Request.Context().Value(perRequestErrorDecoderKey{}).(ErrorDecoder); ok && reqDecoder != nil {
			if reqDecoder.Handles(resp) {
				return reqDecoder.DecodeError(resp)
			}
		}
	}
	return d.clientDecoder.DecodeError(resp)
}
