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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc/conjureerrors"
	"github.com/palantir/conjure-go-runtime/v3/conjure-go-contract/errors"
	"github.com/palantir/pkg/uuid"
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
		httpc.NewGET[struct{}]("Err", "/err").WithDecoder(httpc.VoidDecoder()),
		ced,
	)

	client, err := httpc.NewBuilder().SetBaseURLs(server.URL).Build(context.Background())
	require.NoError(t, err)

	_, _, err = ep.Call().Execute(context.Background(), client)
	require.Error(t, err)
	assert.True(t, ced.called, "custom ConjureErrorDecoder should have been invoked via the endpoint")
}

// TestWithParameterFormat_SetsHeader verifies the helper sets the negotiation header on the
// request, observable end-to-end through Execute.
func TestWithParameterFormat_SetsHeader(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(errors.AcceptConjureErrorParameterFormatHeader)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	ep := conjureerrors.WithParameterFormat(
		httpc.NewGET[struct{}]("Fmt", "/fmt").WithDecoder(httpc.VoidDecoder()),
		errors.ConjureErrorParameterFormatJSON,
	)
	client, err := httpc.NewBuilder().SetBaseURLs(server.URL).Build(context.Background())
	require.NoError(t, err)

	_, _, err = ep.Call().Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, string(errors.ConjureErrorParameterFormatJSON), got)
}

// typedConflictError mimics a conjure-generated typed error with a non-scalar parameter. Its
// UnmarshalJSON fails when "tables" arrives in the legacy string form ("tables":"[basic]") rather
// than a JSON array, which is what triggers the registry's generic-error fallback.
type typedConflictError struct {
	tables []string
}

func (*typedConflictError) Error() string                { return "Test:TablesConflict" }
func (*typedConflictError) Code() errors.ErrorCode       { return errors.Conflict }
func (*typedConflictError) Name() string                 { return "Test:TablesConflict" }
func (*typedConflictError) InstanceID() uuid.UUID        { return uuid.UUID{} }
func (*typedConflictError) SafeParams() map[string]any   { return nil }
func (*typedConflictError) UnsafeParams() map[string]any { return nil }

func (e *typedConflictError) UnmarshalJSON(data []byte) error {
	var se errors.SerializableError
	if err := json.Unmarshal(data, &se); err != nil {
		return err
	}
	var params struct {
		Tables []string `json:"tables"`
	}
	if err := json.Unmarshal(se.Parameters, &params); err != nil {
		return err
	}
	e.tables = params.Tables
	return nil
}

// TestExecute_TypedErrorMalformedParams_FallsBackToGenericError confirms httpc inherits the
// errors-registry fallback (upstream #934) through the Execute path: when a registered typed error
// fails to unmarshal (legacy string params), Execute still yields a usable conjure error that
// preserves name/code/instanceId/raw params, rather than failing to decode.
func TestExecute_TypedErrorMalformedParams_FallsBackToGenericError(t *testing.T) {
	const legacyBody = `{"errorCode":"CONFLICT","errorName":"Test:TablesConflict","errorInstanceId":"ada42104-7688-4720-8e4e-72deae1cec87","parameters":{"tables":"[basic]"}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(legacyBody))
	}))
	defer server.Close()

	ced := errors.NewReflectTypeConjureErrorDecoder().MustRegisterErrorTypes(new(typedConflictError))
	ep := conjureerrors.WithConjureErrorDecoder(
		httpc.NewGET[struct{}]("Conflict", "/conflict").WithDecoder(httpc.VoidDecoder()),
		ced,
	)
	client, err := httpc.NewBuilder().SetBaseURLs(server.URL).Build(context.Background())
	require.NoError(t, err)

	_, _, err = ep.Call().Execute(context.Background(), client)
	require.Error(t, err)

	cerr := errors.GetConjureError(err)
	require.NotNil(t, cerr, "expected a usable conjure error, not a decode failure")
	assert.Equal(t, "Test:TablesConflict", cerr.Name())
	assert.Equal(t, errors.Conflict, cerr.Code())
	assert.Equal(t, "ada42104-7688-4720-8e4e-72deae1cec87", cerr.InstanceID().String())
	assert.Equal(t, "[basic]", cerr.UnsafeParams()["tables"])
}
