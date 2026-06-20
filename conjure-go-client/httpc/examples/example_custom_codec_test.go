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

package examples_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
)

// Example_customCodec plugs in a plain-text encoder and decoder.
//
// NewBodyEncoderFunc and NewBodyDecoderFunc adapt plain functions to the BodyEncoder /
// BodyDecoder interfaces, so any wire format works. The encoder sets the request body
// (and, via the contentType argument, the Content-Type header); the decoder reads the
// response body into the endpoint's response type.
func Example_customCodec() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("content-type:", r.Header.Get("Content-Type"))
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, strings.ToUpper(string(body)))
	}))
	defer server.Close()

	textEncoder := httpc.NewBodyEncoderFunc("text/plain", func(req *http.Request, body string) error {
		req.Body = io.NopCloser(strings.NewReader(body))
		req.ContentLength = int64(len(body))
		req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(body)), nil }
		return nil
	})
	textDecoder := httpc.NewBodyDecoderFunc(func(_ context.Context, resp *http.Response) (string, error) {
		body, err := io.ReadAll(resp.Body)
		return string(body), err
	})

	// Endpoints are values; declare them once and reuse across calls.
	var (
		echo = httpc.NewPOST[string, string]("Echo", "/echo").
			WithEncoder(textEncoder).
			WithDecoder(textDecoder).
			WithAccept("text/plain")
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	reply, _, err := echo.WithBody("ping").Execute(ctx, client)
	if err != nil {
		panic(err)
	}
	fmt.Println("reply:", reply)
	// Output:
	// content-type: text/plain
	// reply: PING
}
