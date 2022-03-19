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

package internal

import (
	"net/http"
	"testing"
	"time"

	werror "github.com/palantir/witchcraft-go-error"
	"github.com/stretchr/testify/assert"
)

func TestRetryResponseParsers(t *testing.T) {
	for _, test := range []struct {
		Name             string
		Response         *http.Response
		RespErr          error
		IsRetryOther     bool
		RetryOtherURL    string
		IsThrottle       bool
		ThrottleDuration time.Duration
		IsUnavailable    bool
	}{
		{
			Name: "200 OK",
			Response: &http.Response{
				Header:     http.Header{},
				StatusCode: 200,
			},
		},
		{
			Name: "307 RetryTemporaryRedirect without Location",
			Response: &http.Response{
				Header:     http.Header{},
				StatusCode: 307,
			},
			IsRetryOther: true,
		},
		{
			Name: "307 RetryTemporaryRedirect with Location",
			Response: &http.Response{
				Header:     http.Header{"Location": []string{"https://host-2:8443/app"}},
				StatusCode: 307,
			},
			IsRetryOther:  true,
			RetryOtherURL: "https://host-2:8443/app",
		},
		{
			Name: "308 RetryOther without Location",
			Response: &http.Response{
				Header:     http.Header{},
				StatusCode: 308,
			},
			IsRetryOther: true,
		},
		{
			Name: "308 RetryOther with Location",
			Response: &http.Response{
				Header:     http.Header{"Location": []string{"https://host-2:8443/app"}},
				StatusCode: 308,
			},
			IsRetryOther:  true,
			RetryOtherURL: "https://host-2:8443/app",
		},
		{
			Name: "307 RetryTemporaryRedirect without Location in error",
			RespErr: werror.Error("error",
				werror.SafeParam("statusCode", 307),
				werror.SafeParam("location", "")),
			IsRetryOther: true,
		},
		{
			Name: "307 RetryTemporaryRedirect with Location in error",
			RespErr: werror.Error("error",
				werror.SafeParam("statusCode", 307),
				werror.SafeParam("location", "https://host-2:8443/app")),
			IsRetryOther: true,
		},
		{
			Name: "429 throttle without Retry-After",
			Response: &http.Response{
				Header:     http.Header{},
				StatusCode: 429,
			},
			IsThrottle: true,
		},
		{
			Name:       "429 throttle in error",
			Response:   nil,
			RespErr:    werror.Error("error", werror.SafeParam("statusCode", 429)),
			IsThrottle: true,
		},
		{
			Name:          "503 unavailable in error",
			Response:      nil,
			RespErr:       werror.Error("error", werror.SafeParam("statusCode", 503)),
			IsUnavailable: true,
		},
		{
			Name: "429 throttle with Retry-After seconds",
			Response: &http.Response{
				Header:     http.Header{"Retry-After": []string{"60"}},
				StatusCode: 429,
			},
			IsThrottle:       true,
			ThrottleDuration: time.Minute,
		},
		{
			Name: "429 throttle with Retry-After Date",
			Response: &http.Response{
				Header:     http.Header{"Retry-After": []string{time.Now().UTC().Add(time.Minute).Format(http.TimeFormat)}},
				StatusCode: 429,
			},
			IsThrottle:       true,
			ThrottleDuration: time.Minute,
		},
	} {
		t.Run(test.Name, func(t *testing.T) {
			errCode, _ := StatusCodeFromError(test.RespErr)
			isRetryOther, retryOtherURL := isRetryOtherResponse1(test.Response, test.RespErr, errCode)
			if assert.Equal(t, test.IsRetryOther, isRetryOther) && test.RetryOtherURL != "" {
				if assert.NotNil(t, retryOtherURL) {
					assert.Equal(t, test.RetryOtherURL, retryOtherURL.String())
				}
			}

			isThrottle, throttleDur := isThrottleResponse(test.Response, errCode)
			if assert.Equal(t, test.IsThrottle, isThrottle) {
				assert.WithinDuration(t, time.Now().Add(test.ThrottleDuration), time.Now().Add(throttleDur), time.Second)
			}

			isUnavailable := isUnavailableResponse(test.Response, errCode)
			assert.Equal(t, test.IsUnavailable, isUnavailable)
		})
	}
}
