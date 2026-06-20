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

package conjureerrors_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc/conjureerrors"
	"github.com/palantir/conjure-go-runtime/v3/conjure-go-contract/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const conjureErrorBody = `{"errorCode":"INVALID_ARGUMENT","errorName":"Conjure:InvalidArgument","errorInstanceId":"00000000-0000-0000-0000-000000000000","parameters":{}}`

type probeConjureDecoder struct{ called bool }

func (p *probeConjureDecoder) DecodeConjureError(string, []byte) (errors.Error, error) {
	p.called = true
	return errors.NewInvalidArgument(), nil
}

// TestDefaultErrorDecoderWithConjure verifies the decoder routes JSON error
// bodies through the provided ConjureErrorDecoder and sets the statusCode param.
func TestDefaultErrorDecoderWithConjure(t *testing.T) {
	ced := &probeConjureDecoder{}
	dec := conjureerrors.DefaultErrorDecoderWithConjure(ced)

	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Status:     "400 Bad Request",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(conjureErrorBody)),
	}
	require.True(t, dec.Handles(resp))

	err := dec.DecodeError(resp)
	require.Error(t, err)
	assert.True(t, ced.called, "custom ConjureErrorDecoder should have been invoked")
	statusCode, ok := httpc.StatusCodeFromError(err)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, statusCode)
}

// TestWithConjureErrorDecoder_OnEndpoint exercises the generic free function on
// an Endpoint end-to-end through Execute.
func TestWithConjureErrorDecoder_OnEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(conjureErrorBody))
	}))
	defer server.Close()

	ced := &probeConjureDecoder{}
	ep := conjureerrors.WithConjureErrorDecoder(
		httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Err", "/err").WithDecoder(httpc.VoidDecoder()),
		ced,
	)

	client, err := httpc.NewBuilder().SetBaseURLs(server.URL).Build(context.Background())
	require.NoError(t, err)

	_, _, err = ep.Execute(context.Background(), client)
	require.Error(t, err)
	assert.True(t, ced.called, "custom ConjureErrorDecoder should have been invoked via the endpoint")
}
