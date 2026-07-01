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
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
)

// Example_formURLEncoded posts an application/x-www-form-urlencoded body.
//
// FormURLEncoder serializes url.Values (via Encode) and sets the Content-Type; the
// endpoint's request type is url.Values.
func Example_formURLEncoded() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		fmt.Println("content-type:", r.Header.Get("Content-Type"))
		fmt.Println("name:", r.PostForm.Get("name"))
		fmt.Println("tags:", r.PostForm["tag"])
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	var submit = httpc.NewPOST[url.Values, struct{}]("Submit", "/submit").
		WithEncoder(httpc.FormURLEncoder()).
		WithDecoder(httpc.VoidDecoder())

	client, err := httpc.NewBuilder().SetServiceName("inventory").SetBaseURLs(server.URL).Build(ctx)
	if err != nil {
		panic(err)
	}

	form := url.Values{"name": {"widget"}, "tag": {"red", "round"}}
	if _, _, err := submit.Call(form).Execute(ctx, client); err != nil {
		panic(err)
	}
	// Output:
	// content-type: application/x-www-form-urlencoded
	// name: widget
	// tags: [red round]
}

// Example_multipartUpload posts a multipart/form-data body with a text field and a file.
//
// MultipartEncoder's request type is a func(*multipart.Writer) error that writes the
// parts; the encoder supplies the writer and sets Content-Type with a fixed boundary.
// The body is streamed (not buffered), and the callback runs once per attempt — reopen
// sources rather than consume one-shot readers if the request may be retried.
func Example_multipartUpload() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			panic(err)
		}
		fmt.Println("name:", r.FormValue("name"))
		file, header, err := r.FormFile("document")
		if err != nil {
			panic(err)
		}
		defer func() { _ = file.Close() }()
		content, _ := io.ReadAll(file)
		fmt.Printf("file %s: %s\n", header.Filename, content)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	var upload = httpc.NewPOST[func(*multipart.Writer) error, struct{}]("Upload", "/upload").
		WithEncoder(httpc.MultipartEncoder()).
		WithDecoder(httpc.VoidDecoder())

	client, err := httpc.NewBuilder().SetServiceName("inventory").SetBaseURLs(server.URL).Build(ctx)
	if err != nil {
		panic(err)
	}

	body := func(mw *multipart.Writer) error {
		if err := mw.WriteField("name", "widget"); err != nil {
			return err
		}
		fw, err := mw.CreateFormFile("document", "spec.txt")
		if err != nil {
			return err
		}
		_, err = io.WriteString(fw, "hello multipart")
		return err
	}
	if _, _, err := upload.Call(body).Execute(ctx, client); err != nil {
		panic(err)
	}
	// Output:
	// name: widget
	// file spec.txt: hello multipart
}
