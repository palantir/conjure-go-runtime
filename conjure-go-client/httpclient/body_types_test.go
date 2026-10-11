// Copyright (c) 2024 Palantir Technologies. All rights reserved.
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
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContentLengthInMemory(t *testing.T) {
	t.Run("bytes.Buffer", func(t *testing.T) {
		assert.EqualValues(t, 5, contentLengthInMemory(bytes.NewBuffer([]byte("hello"))))
	})
	t.Run("bytes.Reader", func(t *testing.T) {
		assert.EqualValues(t, 5, contentLengthInMemory(bytes.NewReader([]byte("hello"))))
	})
	t.Run("strings.Reader", func(t *testing.T) {
		assert.EqualValues(t, 5, contentLengthInMemory(strings.NewReader("hello")))
	})
}

func TestRetrieveRequestBodyReader(t *testing.T) {
	for _, test := range []struct {
		Name          string
		Body          RequestBody
		Expected      string
		ContentLength int64
	}{
		{
			Name:          "RequestBodyInMemory(*bytes.Buffer)",
			Body:          RequestBodyInMemory(bytes.NewBuffer([]byte("hello"))),
			Expected:      "hello",
			ContentLength: 5,
		},
		{
			Name:          "RequestBodyInMemory(*bytes.Reader)",
			Body:          RequestBodyInMemory(bytes.NewReader([]byte("hello"))),
			Expected:      "hello",
			ContentLength: 5,
		},
		{
			Name:          "RequestBodyInMemory(*strings.Reader)",
			Body:          RequestBodyInMemory(strings.NewReader("hello")),
			Expected:      "hello",
			ContentLength: 5,
		},
		{
			Name:          "RequestBodyEmpty()",
			Body:          RequestBodyEmpty(),
			Expected:      "",
			ContentLength: 0,
		},
		{
			Name:          "RequestBodyStreamOnce(func() io.ReadCloser)",
			Body:          RequestBodyStreamOnce(func() io.ReadCloser { return io.NopCloser(strings.NewReader("hello")) }),
			Expected:      "hello",
			ContentLength: -1,
		},
		{
			Name:          "RequestBodyStreamOnce(func() io.ReadCloser { return nil })",
			Body:          RequestBodyStreamOnce(func() io.ReadCloser { return nil }),
			Expected:      "",
			ContentLength: 0,
		},
		{
			Name:          "RequestBodyStreamWithReplay(func() io.ReadCloser)",
			Body:          RequestBodyStreamWithReplay(func() io.ReadCloser { return io.NopCloser(strings.NewReader("hello")) }),
			Expected:      "hello",
			ContentLength: -1,
		},
		{
			Name:          "RequestBodyStreamWithReplay(func() io.ReadCloser { return nil })",
			Body:          RequestBodyStreamWithReplay(func() io.ReadCloser { return nil }),
			Expected:      "",
			ContentLength: 0,
		},
	} {
		t.Run(test.Name, func(t *testing.T) {
			r, l, err := RetrieveReaderFromRequestBody(test.Body)
			require.NoError(t, err)
			assert.EqualValues(t, test.ContentLength, l)
			content, err := io.ReadAll(r)
			require.NoError(t, err)
			assert.Equal(t, test.Expected, string(content))
			if test.ContentLength != -1 {
				assert.Len(t, content, int(l), "content length does not match read bytes")
			}
		})
	}
}

func TestRequestBodyClosesBodyReturnedWithError(t *testing.T) {
	for _, tc := range []struct {
		name string
		body func(io.ReadCloser) RequestBody
	}{
		{
			name: "stream once",
			body: func(body io.ReadCloser) RequestBody {
				return RequestBodyStreamOnce(func() (io.ReadCloser, error) {
					return body, errors.New("failed to create body")
				})
			},
		},
		{
			name: "replayable stream",
			body: func(body io.ReadCloser) RequestBody {
				return RequestBodyStreamWithReplay(func() (io.ReadCloser, error) {
					return body, errors.New("failed to create body")
				})
			},
		},
		{
			name: "stream once with content length",
			body: func(body io.ReadCloser) RequestBody {
				return RequestBodyStreamOnce(func() (io.ReadCloser, int64, error) {
					return body, 7, errors.New("failed to create body")
				})
			},
		},
		{
			name: "replayable stream with content length",
			body: func(body io.ReadCloser) RequestBody {
				return RequestBodyStreamWithReplay(func() (io.ReadCloser, int64, error) {
					return body, 7, errors.New("failed to create body")
				})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &closeTrackingReader{Reader: strings.NewReader("request")}
			reader, _, err := RetrieveReaderFromRequestBody(tc.body(body))

			require.EqualError(t, err, "failed to create body")
			assert.Nil(t, reader)
			assert.Equal(t, 1, body.closes)
		})
	}
}

type closeTrackingReader struct {
	io.Reader
	closes int
}

func (r *closeTrackingReader) Close() error {
	r.closes++
	return nil
}
