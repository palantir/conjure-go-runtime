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

package httpc_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/palantir/conjure-go-runtime/v3/conjure-go-contract/errors"
	"github.com/palantir/pkg/uuid"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errDecoderMaxBodyBytes mirrors httpc's unexported maxErrorBodyBytes (1 MiB).
const errDecoderMaxBodyBytes = 1 << 20

const conjureErrorBody = `{"errorCode":"INVALID_ARGUMENT","errorName":"Conjure:InvalidArgument","errorInstanceId":"00000000-0000-0000-0000-000000000000","parameters":{}}`

func errorResponse(status int, contentType, body string) *http.Response {
	h := make(http.Header)
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// TestDefaultErrorDecoder_CapsErrorBody: DefaultErrorDecoder caps the retained error body at
// maxErrorBodyBytes so a hostile server cannot amplify memory use, flagging truncation via safe params.
func TestDefaultErrorDecoder_CapsErrorBody(t *testing.T) {
	decoder := httpc.DefaultErrorDecoder()

	t.Run("over cap is truncated and flagged", func(t *testing.T) {
		body := strings.Repeat("A", errDecoderMaxBodyBytes+512)
		err := decoder.DecodeError(errorResponse(http.StatusInternalServerError, "text/plain", body))
		require.Error(t, err)
		safe, unsafe := werror.ParamsFromError(err)
		assert.Equal(t, true, safe["responseBodyTruncated"])
		assert.Equal(t, errDecoderMaxBodyBytes, safe["responseBodyLimitBytes"])
		retained, ok := unsafe["responseBody"].(string)
		require.True(t, ok, "responseBody should be present")
		assert.Len(t, retained, errDecoderMaxBodyBytes, "retained body is capped at the limit")
	})

	t.Run("exactly at cap is not truncated", func(t *testing.T) {
		body := strings.Repeat("A", errDecoderMaxBodyBytes)
		err := decoder.DecodeError(errorResponse(http.StatusInternalServerError, "text/plain", body))
		require.Error(t, err)
		safe, unsafe := werror.ParamsFromError(err)
		assert.NotContains(t, safe, "responseBodyTruncated")
		assert.NotContains(t, safe, "responseBodyLimitBytes")
		assert.Len(t, unsafe["responseBody"], errDecoderMaxBodyBytes)
	})

	t.Run("small body is retained intact", func(t *testing.T) {
		err := decoder.DecodeError(errorResponse(http.StatusInternalServerError, "text/plain", "boom"))
		require.Error(t, err)
		safe, unsafe := werror.ParamsFromError(err)
		assert.NotContains(t, safe, "responseBodyTruncated")
		assert.Equal(t, "boom", unsafe["responseBody"])
	})
}

// TestDefaultErrorDecoderWithConjure routes a JSON error body through the provided
// ConjureErrorDecoder and preserves the statusCode param on the returned error.
func TestDefaultErrorDecoderWithConjure(t *testing.T) {
	var called bool
	ced := &probeConjureDecoder{onDecode: func(_ string, body []byte) {
		called = true
		assert.NotEmpty(t, body)
	}}
	dec := httpc.DefaultErrorDecoderWithConjure(ced)

	resp := errorResponse(http.StatusBadRequest, "application/json", conjureErrorBody)
	require.True(t, dec.Handles(resp))

	err := dec.DecodeError(resp)
	require.Error(t, err)
	assert.True(t, called, "custom ConjureErrorDecoder should have been invoked")
	statusCode, ok := httpc.StatusCodeFromError(err)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, statusCode)
}

// TestWithConjureErrorDecoder_OnEndpoint exercises the generic free function on an endpoint
// end-to-end through Execute.
func TestWithConjureErrorDecoder_OnEndpoint(t *testing.T) {
	var called bool
	ced := &probeConjureDecoder{onDecode: func(string, []byte) { called = true }}
	ep := httpc.WithConjureErrorDecoder(
		httpc.NewGET[struct{}]("Err", "/err").WithDecoder(httpc.VoidDecoder()),
		ced,
	)
	client := handlerClient(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(conjureErrorBody))
	})
	_, _, err := ep.Call().Execute(context.Background(), client)
	require.Error(t, err)
	assert.True(t, called, "custom ConjureErrorDecoder should have been invoked via the endpoint")
}

// TestWithConjureErrorParameterFormat_SetsHeader verifies the helper sets the negotiation header
// on the request, observable end-to-end through Execute.
func TestWithConjureErrorParameterFormat_SetsHeader(t *testing.T) {
	var got string
	ep := httpc.WithConjureErrorParameterFormat(
		httpc.NewGET[struct{}]("Fmt", "/fmt").WithDecoder(httpc.VoidDecoder()),
		errors.ConjureErrorParameterFormatJSON,
	)
	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(errors.AcceptConjureErrorParameterFormatHeader)
		w.WriteHeader(http.StatusNoContent)
	})
	_, _, err := ep.Call().Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, string(errors.ConjureErrorParameterFormatJSON), got)
}

// typedConflictError mimics a conjure-generated typed error with a non-scalar parameter. Its
// UnmarshalJSON fails when "tables" arrives in the legacy string form ("tables":"[basic]") rather
// than a JSON array, which triggers the registry's generic-error fallback.
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

// TestExecute_TypedErrorMalformedParams_FallsBackToGenericError confirms the errors-registry
// fallback (upstream #934) survives the Execute path: a registered typed error that fails to
// unmarshal (legacy string params) still yields a usable conjure error via GetConjureError,
// reachable because StatusError wraps the decoded cause.
func TestExecute_TypedErrorMalformedParams_FallsBackToGenericError(t *testing.T) {
	const legacyBody = `{"errorCode":"CONFLICT","errorName":"Test:TablesConflict","errorInstanceId":"ada42104-7688-4720-8e4e-72deae1cec87","parameters":{"tables":"[basic]"}}`
	ced := errors.NewReflectTypeConjureErrorDecoder().MustRegisterErrorTypes(new(typedConflictError))
	ep := httpc.WithConjureErrorDecoder(
		httpc.NewGET[struct{}]("Conflict", "/conflict").WithDecoder(httpc.VoidDecoder()),
		ced,
	)
	client := handlerClient(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(legacyBody))
	})
	_, _, err := ep.Call().Execute(context.Background(), client)
	require.Error(t, err)

	cerr := errors.GetConjureError(err)
	require.NotNil(t, cerr, "expected a usable conjure error, not a decode failure")
	assert.Equal(t, "Test:TablesConflict", cerr.Name())
	assert.Equal(t, errors.Conflict, cerr.Code())
	assert.Equal(t, "ada42104-7688-4720-8e4e-72deae1cec87", cerr.InstanceID().String())
	assert.Equal(t, "[basic]", cerr.UnsafeParams()["tables"])
}

func TestStatusCodeFromError(t *testing.T) {
	for _, tc := range []struct {
		name        string
		err         error
		expectExist bool
		expectCode  int
	}{
		{name: "no status code", err: werror.Error("200"), expectExist: false, expectCode: 0},
		{name: "status code 200", err: werror.Error("200", werror.SafeParam("statusCode", 200)), expectExist: true, expectCode: 200},
		{name: "status code non int", err: werror.Error("200", werror.SafeParam("statusCode", "200")), expectExist: false, expectCode: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, exist := httpc.StatusCodeFromError(tc.err)
			assert.Equal(t, tc.expectExist, exist)
			assert.Equal(t, tc.expectCode, code)
		})
	}
}

func TestLocationFromError(t *testing.T) {
	for _, tc := range []struct {
		name           string
		err            error
		expectExist    bool
		expectLocation string
	}{
		{name: "200 no location", err: werror.Error("200", werror.SafeParam("statusCode", 200)), expectExist: false, expectLocation: ""},
		{name: "307 with location", err: werror.Error("307", werror.SafeParam("statusCode", 307), werror.UnsafeParam("location", "https://google.com")), expectExist: true, expectLocation: "https://google.com"},
		{name: "307 without location", err: werror.Error("307", werror.SafeParam("statusCode", 307), werror.UnsafeParam("location", "")), expectExist: true, expectLocation: ""},
		{name: "307 with non string location", err: werror.Error("307", werror.SafeParam("statusCode", 307), werror.UnsafeParam("location", 12345)), expectExist: false, expectLocation: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			location, exist := httpc.LocationFromError(tc.err)
			assert.Equal(t, tc.expectExist, exist)
			assert.Equal(t, tc.expectLocation, location)
		})
	}
}

// httpc.WithConjureErrorDecoder is equivalent to wrapping with
// httpc.DefaultErrorDecoderWithConjure.
func TestOverrides_WithConjureErrorDecoder(t *testing.T) {
	var called bool
	ced := &probeConjureDecoder{onDecode: func(name string, body []byte) {
		called = true
		assert.NotEmpty(t, body)
	}}

	ep := httpc.NewNoBodyEndpoint[struct{}](http.MethodGet, "Err", "/err").
		WithDecoder(httpc.VoidDecoder())

	client := handlerClient(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errorCode":"INVALID_ARGUMENT","errorName":"Conjure:InvalidArgument","errorInstanceId":"00000000-0000-0000-0000-000000000000","parameters":{}}`))
	})
	_, _, err := ep.Call().WithOverrides(httpc.WithConjureErrorDecoder(httpc.Overrides{}, ced)).Execute(context.Background(), client)
	require.Error(t, err)
	assert.True(t, called, "ConjureErrorDecoder should have been invoked")
}

type probeConjureDecoder struct {
	onDecode func(name string, body []byte)
}

func (p *probeConjureDecoder) DecodeConjureError(name string, body []byte) (errors.Error, error) {
	p.onDecode(name, body)
	return errors.NewInvalidArgument(), nil
}
