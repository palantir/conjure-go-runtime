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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/stretchr/testify/require"
)

func TestRecoveryMiddleware(t *testing.T) {
	helloErr := fmt.Errorf("hello world")

	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	client, err := httpclient.NewClient(
		httpclient.WithBaseURLs([]string{server.URL}),
		httpclient.WithMiddleware(httpclient.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
			panic(helloErr)
		})),
	)
	require.NoError(t, err)

	_, err = client.Do(context.Background(), httpclient.WithRequestMethod(http.MethodGet))
	require.Error(t, err)
	recovered, _ := werror.ParamFromError(err, "recovered")
	require.Equal(t, helloErr.Error(), recovered)
}
