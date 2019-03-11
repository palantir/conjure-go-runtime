// Copyright (c) 2019 Palantir Technologies. All rights reserved.
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

package httpclient_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/palantir/conjure-go-runtime/conjure-go-client/httpclient"
)

var (
	body              = "hello"
	statusCode        = 456
	clientDecoderMsg  = "client custom error decoder error foo"
	requestDecoderMsg = "request custom error decoder error bar"
)

func TestErrorDecoder(t *testing.T) {

	ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(statusCode)
		fmt.Fprint(rw, body)
	}))
	defer ts.Close()
	t.Run("ClientDefault", func(t *testing.T) {
		client, err := httpclient.NewClient(
			httpclient.WithBaseURLs([]string{ts.URL}),
		)
		require.NoError(t, err)
		resp, err := client.Get(context.Background())
		assert.Error(t, err)
		assert.Nil(t, resp)
		gotStatusCode, ok := httpclient.StatusCodeFromError(err)
		assert.True(t, ok)
		assert.Equal(t, statusCode, gotStatusCode)
	})
	t.Run("ClientNoop", func(t *testing.T) {
		client, err := httpclient.NewClient(
			httpclient.WithBaseURLs([]string{ts.URL}),
			httpclient.WithDisableRestErrors(),
		)
		require.NoError(t, err)
		resp, err := client.Get(context.Background())
		assert.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, statusCode, resp.StatusCode)
	})
	t.Run("ClientCustom", func(t *testing.T) {
		client, err := httpclient.NewClient(
			httpclient.WithBaseURLs([]string{ts.URL}),
			httpclient.WithErrorDecoder(&customErrorDecoder{
				statusCode: statusCode,
				message:    clientDecoderMsg,
			}),
		)
		require.NoError(t, err)
		resp, err := client.Get(context.Background())
		assert.Error(t, err)
		assert.Nil(t, resp)
		assert.True(t, strings.Contains(err.Error(), clientDecoderMsg), err.Error())
	})
	t.Run("RequestCustom", func(t *testing.T) {
		client, err := httpclient.NewClient(
			httpclient.WithBaseURLs([]string{ts.URL}),
			httpclient.WithErrorDecoder(&customErrorDecoder{
				statusCode: statusCode,
				message:    clientDecoderMsg,
			}),
		)
		require.NoError(t, err)
		resp, err := client.Get(
			context.Background(),
			httpclient.WithRequestErrorDecoder(&customErrorDecoder{
				statusCode: statusCode,
				message:    requestDecoderMsg,
			}),
		)
		assert.Error(t, err)
		assert.Nil(t, resp)
		assert.True(t, strings.Contains(err.Error(), requestDecoderMsg), err.Error())
		assert.False(t, strings.Contains(err.Error(), clientDecoderMsg), err.Error())
	})
	t.Run("FallbackToClient", func(t *testing.T) {
		client, err := httpclient.NewClient(
			httpclient.WithBaseURLs([]string{ts.URL}),
			httpclient.WithErrorDecoder(&customErrorDecoder{
				statusCode: statusCode,
				message:    clientDecoderMsg,
			}),
		)
		require.NoError(t, err)
		resp, err := client.Get(
			context.Background(),
			httpclient.WithRequestErrorDecoder(&customErrorDecoder{
				statusCode: statusCode + 1, // request error decoder should NOT handle this response
				message:    requestDecoderMsg,
			}),
		)
		assert.Error(t, err)
		assert.Nil(t, resp)
		assert.False(t, strings.Contains(err.Error(), requestDecoderMsg), err.Error())
		assert.True(t, strings.Contains(err.Error(), clientDecoderMsg), err.Error())
	})
}

var _ httpclient.ErrorDecoder = &customErrorDecoder{}

type customErrorDecoder struct {
	statusCode int
	message    string
}

func (ced *customErrorDecoder) Handles(resp *http.Response) bool {
	return ced.statusCode == resp.StatusCode
}

func (ced *customErrorDecoder) DecodeError(resp *http.Response) error {
	return fmt.Errorf(ced.message)
}
