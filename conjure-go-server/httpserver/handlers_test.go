// Copyright (c) 2020 Palantir Technologies. All rights reserved.
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

package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-contract/codecs"
	"github.com/palantir/conjure-go-runtime/v2/conjure-go-contract/errors"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/palantir/witchcraft-go-logging/wlog"
	"github.com/palantir/witchcraft-go-logging/wlog/svclog/svc1log"
	wparams "github.com/palantir/witchcraft-go-params"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler_ServeHTTP(t *testing.T) {
	conjure404Err := errors.NewNotFound(wparams.NewSafeParamStorer(map[string]interface{}{"param": "value"}))
	conjure500Err := errors.NewInternal(wparams.NewSafeParamStorer(map[string]interface{}{"param": "value"}))
	for _, tc := range []struct {
		name       string
		handler    func(http.ResponseWriter, *http.Request) error
		verifyResp func(*testing.T, *http.Response)
		verifyLog  func(*testing.T, []byte)
	}{
		{
			name: "plaintext no error",
			handler: func(rw http.ResponseWriter, req *http.Request) error {
				rw.Header().Add("Content-Type", codecs.Plain.ContentType())
				_, _ = rw.Write([]byte("plaintext"))
				return nil
			},
			verifyResp: func(t *testing.T, resp *http.Response) {
				assert.Equal(t, http.StatusOK, resp.StatusCode)
				body, err := ioutil.ReadAll(resp.Body)
				assert.NoError(t, err)
				assert.Equal(t, "plaintext", string(body))
			},
			verifyLog: func(t *testing.T, i []byte) {
				assert.Empty(t, string(i))
			},
		},
		{
			name: "500 plaintext error",
			handler: func(rw http.ResponseWriter, req *http.Request) error {
				return werror.Error("a bad thing", werror.SafeParam("param", "value"))
			},
			verifyResp: func(t *testing.T, resp *http.Response) {
				assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
				assert.Equal(t, "text/plain; charset=utf-8", resp.Header.Get("Content-Type"))
				body, err := ioutil.ReadAll(resp.Body)
				assert.NoError(t, err)
				assert.Equal(t, "a bad thing\n", string(body))
			},
			verifyLog: func(t *testing.T, i []byte) {
				logLine := map[string]interface{}{}
				err := codecs.JSON.Unmarshal(i, &logLine)
				require.NoError(t, err)
				assert.Equal(t, "ERROR", logLine["level"])
				assert.Equal(t, "error handling request: a bad thing", logLine["message"])
				assert.Equal(t, map[string]interface{}{"param": "value"}, logLine["params"])
			},
		},
		{
			name: "404 legacy plaintext error",
			handler: func(rw http.ResponseWriter, req *http.Request) error {
				return werror.Error("a bad thing", werror.SafeParam("param", "value"), werror.SafeParam(legacyHTTPStatusCodeParamKey, http.StatusNotFound))
			},
			verifyResp: func(t *testing.T, resp *http.Response) {
				assert.Equal(t, http.StatusNotFound, resp.StatusCode)
				assert.Equal(t, "text/plain; charset=utf-8", resp.Header.Get("Content-Type"))
				body, err := ioutil.ReadAll(resp.Body)
				assert.NoError(t, err)
				assert.Equal(t, "a bad thing\n", string(body))
			},
			verifyLog: func(t *testing.T, i []byte) {
				logLine := map[string]interface{}{}
				err := codecs.JSON.Unmarshal(i, &logLine)
				require.NoError(t, err)
				assert.Equal(t, "INFO", logLine["level"])
				assert.Equal(t, "error handling request: a bad thing", logLine["message"])
				assert.Equal(t, map[string]interface{}{"param": "value", "httpStatusCode": json.Number("404")}, logLine["params"])
			},
		},
		{
			name: "500 conjure error",
			handler: func(rw http.ResponseWriter, req *http.Request) error {
				return conjure500Err
			},
			verifyResp: func(t *testing.T, resp *http.Response) {
				assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
				assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
				body, err := ioutil.ReadAll(resp.Body)
				assert.NoError(t, err)
				expected, err := conjure500Err.(json.Marshaler).MarshalJSON()
				assert.NoError(t, err)
				assert.JSONEq(t, string(expected), string(body))
			},
			verifyLog: func(t *testing.T, i []byte) {
				logLine := map[string]interface{}{}
				err := codecs.JSON.Unmarshal(i, &logLine)
				require.NoError(t, err)
				assert.Equal(t, "ERROR", logLine["level"])
				assert.Equal(t, fmt.Sprintf("error handling request: INTERNAL Default:Internal (%v)", conjure500Err.InstanceID()), logLine["message"])
				assert.Equal(t, map[string]interface{}{"param": "value", "errorInstanceId": conjure500Err.InstanceID().String()}, logLine["params"])
			},
		},
		{
			name: "404 conjure error",
			handler: func(rw http.ResponseWriter, req *http.Request) error {
				return conjure404Err
			},
			verifyResp: func(t *testing.T, resp *http.Response) {
				assert.Equal(t, http.StatusNotFound, resp.StatusCode)
				assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
				body, err := ioutil.ReadAll(resp.Body)
				assert.NoError(t, err)
				expected, err := conjure404Err.(json.Marshaler).MarshalJSON()
				assert.NoError(t, err)
				assert.JSONEq(t, string(expected), string(body))
			},
			verifyLog: func(t *testing.T, i []byte) {
				logLine := map[string]interface{}{}
				err := codecs.JSON.Unmarshal(i, &logLine)
				require.NoError(t, err)
				assert.Equal(t, "INFO", logLine["level"])
				assert.Equal(t, fmt.Sprintf("error handling request: NOT_FOUND Default:NotFound (%v)", conjure404Err.InstanceID()), logLine["message"])
				assert.Equal(t, map[string]interface{}{"param": "value", "errorInstanceId": conjure404Err.InstanceID().String()}, logLine["params"])
			},
		},
		{
			name: "404 conjure error, wrapped",
			handler: func(rw http.ResponseWriter, req *http.Request) error {
				return werror.Wrap(conjure404Err, "a bad thing", werror.UnsafeParam("unsafeParam", "unsafeValue"))
			},
			verifyResp: func(t *testing.T, resp *http.Response) {
				assert.Equal(t, http.StatusNotFound, resp.StatusCode)
				assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
				body, err := ioutil.ReadAll(resp.Body)
				assert.NoError(t, err)
				expected, err := conjure404Err.(json.Marshaler).MarshalJSON()
				assert.NoError(t, err)
				assert.JSONEq(t, string(expected), string(body))
			},
			verifyLog: func(t *testing.T, i []byte) {
				logLine := map[string]interface{}{}
				err := codecs.JSON.Unmarshal(i, &logLine)
				require.NoError(t, err)
				assert.Equal(t, "INFO", logLine["level"])
				assert.Equal(t, fmt.Sprintf("error handling request: a bad thing: NOT_FOUND Default:NotFound (%v)", conjure404Err.InstanceID()), logLine["message"])
				assert.Equal(t, map[string]interface{}{"param": "value", "errorInstanceId": conjure404Err.InstanceID().String()}, logLine["params"])
				assert.Equal(t, map[string]interface{}{"unsafeParam": "unsafeValue"}, logLine["unsafeParams"])
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logBuf bytes.Buffer
			wlog.NewJSONMarshalLoggerProvider()
			req, err := http.NewRequest(http.MethodGet, "", nil)
			require.NoError(t, err)
			req = req.WithContext(svc1log.WithLogger(context.Background(),
				svc1log.NewFromCreator(&logBuf, wlog.InfoLevel, wlog.NewJSONMarshalLoggerProvider().NewLeveledLogger, svc1log.Origin("")),
			))

			recorder := httptest.NewRecorder()
			handler := NewJSONHandler(tc.handler, StatusCodeMapper, ErrHandler)
			handler.ServeHTTP(recorder, req)
			tc.verifyResp(t, recorder.Result())
			tc.verifyLog(t, logBuf.Bytes())
		})
	}
}

func TestStatusCodeMapper(t *testing.T) {
	for _, tc := range []struct {
		name         string
		err          error
		expectedCode int
	}{
		{
			name:         "conjure not found error",
			err:          errors.NewNotFound(),
			expectedCode: http.StatusNotFound,
		},
		{
			name:         "wrapped conjure not found error",
			err:          werror.Wrap(errors.NewNotFound(), "not found"),
			expectedCode: http.StatusNotFound,
		},
		{
			name:         "wrapped conjure not found error with legacy code",
			err:          werror.Wrap(errors.NewNotFound(), "not found", werror.SafeParam(legacyHTTPStatusCodeParamKey, http.StatusInternalServerError)),
			expectedCode: http.StatusNotFound,
		},
		{
			name:         "legacy not found httpserver error",
			err:          werror.Error("Test error", werror.SafeParam(legacyHTTPStatusCodeParamKey, http.StatusNotFound)),
			expectedCode: http.StatusNotFound,
		},
		{
			name:         "wrapped legacy not found error",
			err:          werror.Wrap(werror.Error("Test error", werror.SafeParam(legacyHTTPStatusCodeParamKey, http.StatusNotFound)), "outer"),
			expectedCode: http.StatusNotFound,
		},
		{
			name:         "no httpserver error",
			err:          werror.Error("werror"),
			expectedCode: http.StatusInternalServerError,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, StatusCodeMapper(tc.err), tc.expectedCode)
		})
	}
}
