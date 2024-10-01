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
	"runtime"
	"runtime/pprof"
	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClose(t *testing.T) {

	// create test server and client with an HTTP Timeout of 5s
	ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintln(rw, "test")
	}))
	client, err := httpclient.NewClient(
		httpclient.WithBaseURL(ts.URL),
		httpclient.WithHTTPTimeout(5*time.Second),
	)
	require.NoError(t, err)

	// execute a simple request
	ctx := context.Background()
	_, err = client.Get(ctx, httpclient.WithPath("/"))
	require.NoError(t, err)

	// check for bad goroutine before timeout is over
	time.Sleep(100 * time.Millisecond) // leave some time for the goroutine to reasonably exit
	buf := bytes.NewBuffer(nil)
	require.NoError(t, pprof.Lookup("goroutine").WriteTo(buf, 1))
	s := buf.String()
	assert.NotContains(t, s, "net/http.setRequestCancel")
}

func TestCloseOnError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(400)
		_, _ = fmt.Fprintln(rw, "test")
	}))
	defer ts.Close()
	before := runtime.NumGoroutine()
	// create test server and client with an HTTP Timeout of 5s
	client, err := httpclient.NewClient(
		httpclient.WithBaseURL(ts.URL),
		httpclient.WithHTTPTimeout(5*time.Second),
	)
	require.NoError(t, err)

	// execute a simple request
	ctx := context.Background()
	_, err = client.Get(
		ctx,
		httpclient.WithPath("/"),
		httpclient.WithHeader("Connection", "close "),
	)
	require.Error(t, err)

	// check for bad goroutine before timeout is over
	time.Sleep(100 * time.Millisecond) // leave some time for the goroutine to reasonably exit
	buf := bytes.NewBuffer(nil)
	require.NoError(t, pprof.Lookup("goroutine").WriteTo(buf, 1))
	s := buf.String()
	after := runtime.NumGoroutine()
	assert.Equal(t, before, after, s)
}

func TestCloseOnEmptyResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(400)
	}))
	defer ts.Close()
	before := runtime.NumGoroutine()
	// create test server and client with an HTTP Timeout of 5s
	client, err := httpclient.NewClient(
		httpclient.WithBaseURL(ts.URL),
		httpclient.WithHTTPTimeout(5*time.Second),
	)
	require.NoError(t, err)

	// execute a simple request
	ctx := context.Background()
	_, err = client.Get(
		ctx,
		httpclient.WithPath("/"),
		httpclient.WithHeader("Connection", "close "),
	)
	require.Error(t, err)

	// check for bad goroutine before timeout is over
	time.Sleep(1000 * time.Millisecond) // leave some time for the goroutine to reasonably exit
	buf := bytes.NewBuffer(nil)
	require.NoError(t, pprof.Lookup("goroutine").WriteTo(buf, 1))
	s := buf.String()
	after := runtime.NumGoroutine()
	assert.Equal(t, before, after, s)
}

func TestStreamingResponse(t *testing.T) {
	const (
		firstLine  = "alpha"
		secondLine = "bravo"
	)
	finishResponseChan := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		f, ok := rw.(http.Flusher)
		require.True(t, ok)
		_, err := fmt.Fprintln(rw, firstLine)
		f.Flush()
		require.NoError(t, err)
		<-finishResponseChan
		_, err = fmt.Fprintln(rw, secondLine)
		require.NoError(t, err)
		f.Flush()
	}))
	defer ts.Close()
	client, err := httpclient.NewClient(
		httpclient.WithBaseURL(ts.URL),
	)
	require.NoError(t, err)
	ctx := context.Background()
	resp, err := client.Get(ctx, httpclient.WithPath("/"), httpclient.WithRawResponseBody())
	require.NoError(t, err)
	close(finishResponseChan)
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, firstLine+"\n"+secondLine+"\n", string(b))
}
