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

// Example_binaryStreaming uploads and downloads raw bytes without buffering as a value.
//
// BinaryEncoder sends an io.ReadCloser as the request body; BinaryDecoder hands the
// response body back as an io.ReadCloser for the caller to stream and close. For
// retryable uploads see Example_replayableStreamingBody (BinaryEncoder probes for a
// seekable/named source; BinaryEncoderOnce and BinaryEncoderWithReplay are explicit).
func Example_binaryStreaming() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uploaded, _ := io.ReadAll(r.Body)
		fmt.Printf("uploaded %d bytes\n", len(uploaded))
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(uploaded) // echo it back as the download
	}))
	defer server.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		upload = httpc.NewPUT[io.ReadCloser, io.ReadCloser]("Upload", "/blobs/widget").
			WithEncoder(httpc.BinaryEncoder("application/octet-stream")).
			WithDecoder(httpc.BinaryDecoder())
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	payload := io.NopCloser(strings.NewReader("hello, world"))
	download, _, err := upload.WithBody(payload).Execute(ctx, client)
	if err != nil {
		panic(err)
	}
	defer func() { _ = download.Close() }()

	got, err := io.ReadAll(download)
	if err != nil {
		panic(err)
	}
	fmt.Printf("downloaded: %s\n", got)
	// Output:
	// uploaded 12 bytes
	// downloaded: hello, world
}
