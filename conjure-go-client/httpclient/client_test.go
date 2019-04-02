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
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClientImpl_DoRetryWorks(t *testing.T) {
	count := 0
	requestBytes := []byte{12, 13}

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		gotReqBytes, err := ioutil.ReadAll(req.Body)
		assert.NoError(t, err)
		assert.Equal(t, requestBytes, gotReqBytes)
		if count == 0 {
			rw.WriteHeader(http.StatusBadRequest)
		}
		// Otherwise 200 is returned
		count++
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURLs([]string{server.URL}))
	assert.NoError(t, err)

	_, err = client.Do(
		context.Background(),
		WithRawRequestBodyProvider(func() io.ReadCloser {
			return ioutil.NopCloser(bytes.NewReader(requestBytes))
		}),
		WithRequestMethod(http.MethodPost))
	assert.NoError(t, err)
	assert.Equal(t, 2, count)
}
