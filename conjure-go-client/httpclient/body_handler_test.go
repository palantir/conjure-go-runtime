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

package httpclient_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient"
	"github.com/palantir/conjure-go-runtime/v2/conjure-go-contract/codecs"
	"github.com/palantir/pkg/bytesbuffers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONBody(t *testing.T) {
	reqVar := map[string]string{"1": "2"}
	respVar := map[string]string{"3": "4"}

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "TestNewRequest", req.Header.Get("User-Agent"))
		var actualReqVar map[string]string
		err := codecs.JSON.Decode(req.Body, &actualReqVar)
		assert.NoError(t, err)
		assert.Equal(t, reqVar, actualReqVar)

		err = codecs.JSON.Encode(rw, respVar)
		assert.NoError(t, err)
	}))
	defer server.Close()

	client, err := httpclient.NewClient(
		httpclient.WithUserAgent("TestNewRequest"),
		httpclient.WithBaseURLs([]string{server.URL}),
	)
	require.NoError(t, err)

	var actualRespVar map[string]string
	resp, err := client.Do(context.Background(),
		httpclient.WithRequestMethod(http.MethodPost),
		httpclient.WithJSONRequest(&reqVar),
		httpclient.WithJSONResponse(&actualRespVar),
	)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, respVar, actualRespVar)
}

func TestRawBody(t *testing.T) {
	reqVar := []byte{0x01, 0x00}
	respVar := []byte{0x00, 0x01}

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "TestNewRequest", req.Header.Get("User-Agent"))
		gotReqBytes, err := io.ReadAll(req.Body)
		assert.NoError(t, err)
		assert.Equal(t, gotReqBytes, reqVar)
		_, err = rw.Write(respVar)
		assert.NoError(t, err)
	}))
	defer server.Close()

	client, err := httpclient.NewClient(
		httpclient.WithUserAgent("TestNewRequest"),
		httpclient.WithBaseURLs([]string{server.URL}),
	)
	require.NoError(t, err)

	resp, err := client.Do(context.Background(),
		httpclient.WithRequestMethod(http.MethodPost),
		httpclient.WithBinaryRequestBody(httpclient.RequestBodyInMemory(bytes.NewBuffer(reqVar))),
		httpclient.WithRawResponseBody(),
	)
	assert.NoError(t, err)

	gotRespBytes, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	defer func() {
		_ = resp.Body.Close()
	}()

	assert.NotNil(t, resp)
	assert.Equal(t, respVar, gotRespBytes)
}

func TestRawRequestRetry(t *testing.T) {
	count := 0
	requestBytes := []byte{12, 13}

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		gotReqBytes, err := io.ReadAll(req.Body)
		assert.NoError(t, err)
		assert.Equal(t, requestBytes, gotReqBytes)
		if count == 0 {
			rw.WriteHeader(http.StatusInternalServerError)
		}
		// Otherwise 200 is returned
		count++
	}))
	defer server.Close()

	client, err := httpclient.NewClient(httpclient.WithBaseURLs([]string{server.URL}))
	assert.NoError(t, err)

	_, err = client.Do(
		context.Background(),
		httpclient.WithBinaryRequestBody(httpclient.RequestBodyInMemory(bytes.NewReader(requestBytes))),
		httpclient.WithRequestMethod(http.MethodPost))
	assert.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestRedirectWithBodyAndBytesBuffer(t *testing.T) {
	reqVar := map[string]string{"1": "2"}
	respVar := map[string]string{"3": "4"}

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		var actualReqVar map[string]string
		err := codecs.JSON.Decode(req.Body, &actualReqVar)
		if assert.NoError(t, err) {
			assert.Equal(t, reqVar, actualReqVar)
		} else {
			t.Log(err)
			rw.WriteHeader(http.StatusInternalServerError)
			return
		}

		switch req.URL.Path {
		case "/redirect":
			rw.Header().Add("Location", "/location")
			rw.WriteHeader(307)
		case "/location":
			assert.NoError(t, codecs.JSON.Encode(rw, respVar))
		default:
			rw.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := httpclient.NewClient(
		httpclient.WithUserAgent("TestNewRequest"),
		httpclient.WithBaseURLs([]string{server.URL}),
		httpclient.WithBytesBufferPool(bytesbuffers.NewSizedPool(1, 10)),
	)
	require.NoError(t, err)

	t.Run("WithJSONRequest", func(t *testing.T) {
		var actualRespVar map[string]string
		resp, err := client.Do(context.Background(),
			httpclient.WithRequestMethod(http.MethodPost),
			httpclient.WithPath("/redirect"),
			httpclient.WithJSONRequest(&reqVar),
			httpclient.WithJSONResponse(&actualRespVar),
		)

		require.NoError(t, err)
		assert.NotNil(t, resp)
		if assert.NotNil(t, resp) {
			assert.Equal(t, resp.StatusCode, 200)
		}
		assert.Equal(t, respVar, actualRespVar)
	})

	t.Run("RequestBodyInMemory[*strings.Reader]", func(t *testing.T) {
		var actualRespVar map[string]string
		resp, err := client.Do(context.Background(),
			httpclient.WithRequestMethod(http.MethodPost),
			httpclient.WithPath("/redirect"),
			httpclient.WithBinaryRequestBody(httpclient.RequestBodyInMemory(strings.NewReader(`{"1":"2"}`))),
			httpclient.WithJSONResponse(&actualRespVar),
		)

		require.NoError(t, err)
		assert.NotNil(t, resp)
		if assert.NotNil(t, resp) {
			assert.Equal(t, resp.StatusCode, 200)
		}
		assert.Equal(t, respVar, actualRespVar)
	})

	t.Run("RequestBodyStreamWithReplay", func(t *testing.T) {
		var actualRespVar map[string]string
		resp, err := client.Do(context.Background(),
			httpclient.WithRequestMethod(http.MethodPost),
			httpclient.WithPath("/redirect"),
			httpclient.WithBinaryRequestBody(httpclient.RequestBodyStreamWithReplay(func() (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader(`{"1":"2"}`)), nil
			})),
			httpclient.WithJSONResponse(&actualRespVar),
		)

		require.NoError(t, err)
		if assert.NotNil(t, resp) {
			assert.Equal(t, resp.StatusCode, 200)
		}
		assert.Equal(t, respVar, actualRespVar)
	})

	t.Run("RequestBodyStreamOnce posts body", func(t *testing.T) {
		var actualRespVar map[string]string
		resp, err := client.Do(context.Background(),
			httpclient.WithRequestMethod(http.MethodPost),
			httpclient.WithPath("/location"),
			httpclient.WithBinaryRequestBody(httpclient.RequestBodyStreamOnce(func() (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader(`{"1":"2"}`)), nil
			})),
			httpclient.WithJSONResponse(&actualRespVar),
		)

		require.NoError(t, err)
		if assert.NotNil(t, resp) {
			assert.Equal(t, resp.StatusCode, 200)
		}
		assert.Equal(t, respVar, actualRespVar)
	})

	t.Run("RequestBodyStreamOnce does not follow redirect", func(t *testing.T) {
		var readOnce bool
		var actualRespVar map[string]string
		resp, err := client.Do(context.Background(),
			httpclient.WithRequestMethod(http.MethodPost),
			httpclient.WithPath("/redirect"),
			httpclient.WithBinaryRequestBody(httpclient.RequestBodyStreamOnce(func() (io.ReadCloser, error) {
				if readOnce {
					return nil, fmt.Errorf("readOnce is true")
				}
				readOnce = true
				return io.NopCloser(strings.NewReader(`{"1":"2"}`)), nil
			})),
			httpclient.WithJSONResponse(&actualRespVar),
		)

		require.EqualError(t, err, "httpclient request failed: 307 Temporary Redirect")
		assert.Nil(t, resp)
	})

	t.Run("RequestBodyStreamOnce does not retry on 404", func(t *testing.T) {
		var readOnce bool
		var actualRespVar map[string]string
		resp, err := client.Do(context.Background(),
			httpclient.WithRequestMethod(http.MethodPost),
			httpclient.WithPath("/invalid"),
			httpclient.WithBinaryRequestBody(httpclient.RequestBodyStreamOnce(func() (io.ReadCloser, error) {
				if readOnce {
					return nil, fmt.Errorf("readOnce is true")
				}
				readOnce = true
				return io.NopCloser(strings.NewReader(`{"1":"2"}`)), nil
			})),
			httpclient.WithJSONResponse(&actualRespVar),
		)

		require.EqualError(t, err, "httpclient request failed: 404 Not Found")
		assert.Nil(t, resp)
	})
}
