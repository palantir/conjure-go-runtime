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

package httpclient

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientBalancedScoringAttributesBasePath(t *testing.T) {
	healthyServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))
	defer healthyServer.Close()

	requestPath := make(chan string, 1)
	failingServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		requestPath <- req.URL.Path
		rw.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer failingServer.Close()

	healthyURI := healthyServer.URL + "/healthy/base"
	failingURI := failingServer.URL + "/failing/base"
	httpClient, err := NewClient(WithBaseURLs([]string{healthyURI, failingURI}), WithNoProxy())
	require.NoError(t, err)

	client := httpClient.(*clientImpl)
	_, _, err = client.doOnce(
		t.Context(),
		failingURI,
		false,
		WithRequestMethod(http.MethodGet),
		WithPath("/rpc"),
	)
	require.Error(t, err)
	assert.Equal(t, "/failing/base/rpc", <-requestPath)
	assert.Equal(t,
		[]string{healthyURI, failingURI},
		client.uriScorer.CurrentURIScoringMiddleware().GetURIsInOrderOfIncreasingScore(),
	)
}
