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

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
)

// Example_greedyPathParam contrasts a greedy {param*} placeholder with a plain {param}.
//
// A plain {name} escapes the whole value, so a slash in it becomes %2F and stays one
// path segment. A trailing greedy {name*} preserves slashes as path separators while
// still escaping each segment individually (a space becomes %20). Use the greedy form
// for file-path-like parameters that span multiple segments.
func Example_greedyPathParam() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println(r.RequestURI)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		greedy = httpc.NewJSONGET[struct{}]("GetFile", "/files/{filePath*}")
		plain  = httpc.NewJSONGET[struct{}]("GetByName", "/lookup/{name}")
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	value := "my docs/report.txt"

	if _, _, err = greedy.WithPathParam("filePath", value).Execute(ctx, client); err != nil {
		panic(err)
	}
	if _, _, err = plain.WithPathParam("name", value).Execute(ctx, client); err != nil {
		panic(err)
	}
	// Output:
	// /files/my%20docs/report.txt
	// /lookup/my%20docs%2Freport.txt
}
