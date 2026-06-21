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
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
)

// Example_middlewareRequestSigning signs each request body in a middleware.
//
// To sign the body a middleware must read it — which drains the reader — and then
// restore it so the transport can still send it. The middleware runs per attempt, so
// retries are re-read and re-signed. Here the signature is an HMAC over the request
// body, which the server recomputes and compares.
func Example_middlewareRequestSigning() {
	ctx := context.Background()
	secret := []byte("shared-secret")
	sign := func(body []byte) string {
		mac := hmac.New(sha256.New, secret)
		mac.Write(body)
		return hex.EncodeToString(mac.Sum(nil))
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got := r.Header.Get("X-Signature")
		fmt.Println("signature valid:", hmac.Equal([]byte(got), []byte(sign(body))))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	signRequests := httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		if req.Body != nil {
			body, err := io.ReadAll(req.Body)
			_ = req.Body.Close()
			if err != nil {
				return nil, err
			}
			req.Header.Set("X-Signature", sign(body))
			req.Body = io.NopCloser(bytes.NewReader(body)) // restore the drained body
		}
		return next.RoundTrip(req)
	})

	// Endpoints are values; declare them once and reuse across calls.
	var (
		putItem = httpc.NewPUT[signingItem, struct{}]("PutItem", "/items/widget").WithJSON()
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		AddMiddleware(signRequests).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	if _, _, err = putItem.Call(signingItem{Name: "widget"}).Execute(ctx, client); err != nil {
		panic(err)
	}
	// Output:
	// signature valid: true
}

type signingItem struct {
	Name string `json:"name"`
}
