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
	"sync/atomic"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
)

// Example_replayableStreamingBody shows which binary encoders make a streamed upload
// retryable.
//
// Retrying a request means resending its body, so the client only retries when it can
// reopen the stream. BinaryEncoderWithReplay takes a factory and sets GetBody, so each
// retry gets a fresh reader. BinaryEncoderOnce sends the reader a single time and leaves
// GetBody unset, so a failed attempt is not retried. (BinaryEncoder is the middle
// ground: it probes a seekable, named source like an *os.File and replays that.)
func Example_replayableStreamingBody() {
	ctx := context.Background()
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if attempts.Add(1) == 1 {
			http.Error(w, "warming up", http.StatusServiceUnavailable) // fail the first attempt only
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	newClient := func() httpc.Client {
		client, err := httpc.NewBuilder().
			SetServiceName("inventory").
			SetBaseURLs(server.URL).
			SetMaxAttempts(new(3)).
			SetInitialBackoff(time.Millisecond).
			SetMaxBackoff(time.Millisecond).
			Build(ctx)
		if err != nil {
			panic(err)
		}
		return client
	}

	// Endpoints are values; declare them once and reuse across calls.
	var (
		replayUpload = httpc.NewPUT[func() (io.ReadCloser, error), struct{}]("Upload", "/blobs/widget").
				WithEncoder(httpc.BinaryEncoderWithReplay("application/octet-stream")).
				WithDecoder(httpc.VoidDecoder())
		onceUpload = httpc.NewPUT[io.ReadCloser, struct{}]("Upload", "/blobs/widget").
				WithEncoder(httpc.BinaryEncoderOnce("application/octet-stream")).
				WithDecoder(httpc.VoidDecoder())
	)

	// With replay, the 503 first attempt is retried with a fresh stream and succeeds.
	open := func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("payload")), nil }
	if _, _, err := replayUpload.WithBody(open).Execute(ctx, newClient()); err != nil {
		panic(err)
	}
	fmt.Println("with replay, attempts:", attempts.Load())

	// Without replay the stream can't be reopened, so the 503 is returned as-is.
	attempts.Store(0)
	_, _, err := onceUpload.WithBody(io.NopCloser(strings.NewReader("payload"))).Execute(ctx, newClient())
	fmt.Println("without replay, attempts:", attempts.Load())
	fmt.Println("without replay, failed:", err != nil)
	// Output:
	// with replay, attempts: 2
	// without replay, attempts: 1
	// without replay, failed: true
}
