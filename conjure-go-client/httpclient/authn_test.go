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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoundTripperWithToken(t *testing.T) {
	var wrappedRTInvoked bool
	tokenProvider := httpclient.TokenProvider(func(_ context.Context) (string, error) {
		return "foo", nil
	})

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		wrappedRTInvoked = true
		assert.Equal(t, "Bearer foo", req.Header.Get("Authorization"))
		rw.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := httpclient.NewClient(
		httpclient.WithHTTPTimeout(time.Minute),
		httpclient.WithAuthTokenProvider(tokenProvider),
		httpclient.WithBaseURLs([]string{server.URL}))
	require.NoError(t, err)

	resp, err := client.Do(context.Background(), httpclient.WithRequestMethod(http.MethodGet))
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, wrappedRTInvoked)
}

func TestRoundTripperWithBasicAuth(t *testing.T) {
	var wrappedRTInvoked bool
	expected := httpclient.BasicAuth{
		User:     "user",
		Password: "password",
	}

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		wrappedRTInvoked = true
		user, pass, ok := req.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, expected.User, user)
		assert.Equal(t, expected.Password, pass)
		rw.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := httpclient.NewClient(
		httpclient.WithHTTPTimeout(time.Minute),
		httpclient.WithBasicAuth(expected.User, expected.Password),
		httpclient.WithBaseURLs([]string{server.URL}))
	require.NoError(t, err)

	resp, err := client.Do(context.Background(), httpclient.WithRequestMethod(http.MethodGet))
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, wrappedRTInvoked)

}
