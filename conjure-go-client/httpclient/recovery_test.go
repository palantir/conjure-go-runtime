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
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/stretchr/testify/require"
)

func TestRecoveryMiddleware(t *testing.T) {
	helloErr := fmt.Errorf("hello world")

	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	client, err := httpclient.NewClient(
		httpclient.WithBaseURLs([]string{server.URL}),
		httpclient.WithMaxRetries(0),
		httpclient.WithMiddleware(panicMiddleware{err: helloErr}),
	)
	require.NoError(t, err)

	body := &closeCountingRequestBody{Reader: strings.NewReader("request")}
	_, err = client.Do(
		context.Background(),
		httpclient.WithRequestMethod(http.MethodPost),
		httpclient.WithBinaryRequestBody(httpclient.RequestBodyStreamOnce(func() io.ReadCloser { return body })),
	)
	require.Error(t, err)
	recovered, _ := werror.ParamFromError(err, "recovered")
	require.Equal(t, helloErr.Error(), recovered)
	require.Equal(t, 1, body.closes)
}

type panicMiddleware struct{ err error }

func (p panicMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	panic(p.err)
}
