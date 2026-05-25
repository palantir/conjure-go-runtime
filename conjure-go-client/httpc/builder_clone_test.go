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

package httpc

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/internal"
	"github.com/palantir/pkg/metrics"
	"github.com/palantir/pkg/refreshable/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nonZeroBuilder returns a Builder with every field set to a distinguishable
// non-zero value. If you add a field to Builder, populate it here too — the
// sanity loop in TestBuilder_ClonePreservesAllFields fails otherwise, which
// in turn ensures the Clone preservation check actually exercises the field.
func nonZeroBuilder() *Builder {
	one := 1
	return &Builder{
		serviceName:     refreshable.New("svc"),
		timeout:         refreshable.New(time.Second),
		dialerOverride:  &net.Dialer{Timeout: time.Second},
		dialerParams:    refreshable.New(dialerParams{DialTimeout: time.Second}),
		tlsConfig:       &tls.Config{InsecureSkipVerify: true},
		transportParams: refreshable.New(transportParams{MaxIdleConns: 1}),
		tlsFileParams:   refreshable.New(tlsFileParams{InsecureSkipVerify: true}),
		tlsCABytes:      refreshable.New([][]byte{{1}}),

		middlewares:      []Middleware{MiddlewareFunc(func(*http.Request, http.RoundTripper) (*http.Response, error) { return nil, nil })},
		innerMiddlewares: []Middleware{MiddlewareFunc(func(*http.Request, http.RoundTripper) (*http.Response, error) { return nil, nil })},
		authHeader:       func(context.Context) (string, error) { return "Bearer x", nil },

		disableMetrics:      refreshable.New(true),
		metricsTagProviders: []TagsProvider{TagsProviderFunc(func(*http.Request, *http.Response, error) metrics.Tags { return nil })},
		disableRequestSpan:  true,
		disableRecovery:     true,
		disableTraceHeaders: true,
		disableTraceMetrics: true,

		uris:             refreshable.New([]string{"https://x"}),
		uriScorerBuilder: func([]string) internal.URIScoringMiddleware { return nil },
		allowEmptyURIs:   true,

		errorDecoder:    defaultRestErrorDecoder{},
		bytesBufferPool: BufferPoolSmall,
		maxAttempts:     refreshable.New(&one),
		initialBackoff:  refreshable.New(time.Second),
		maxBackoff:      refreshable.New(2 * time.Second),

		transport:        http.DefaultTransport,
		caByteSlices:     [][]byte{{1, 2, 3}},
		clientCertKey:    []byte{4, 5, 6},
		clientCertCert:   []byte{7, 8, 9},
		includeSystemCAs: true,
		errs:             []error{errors.New("oops")},
	}
}

// TestBuilder_ClonePreservesAllFields catches the most common Clone() bug
// class: a field added to Builder but not propagated to Clone. The test
// requires every Builder field be non-zero in nonZeroBuilder so the
// post-Clone IsZero check actually exercises preservation.
func TestBuilder_ClonePreservesAllFields(t *testing.T) {
	src := nonZeroBuilder()

	// Sanity: nonZeroBuilder must populate every Builder field. If you added a
	// field and didn't populate it here, this fails before we even Clone.
	srcVal := reflect.ValueOf(src).Elem()
	srcType := srcVal.Type()
	for i := 0; i < srcVal.NumField(); i++ {
		name := srcType.Field(i).Name
		require.False(t, srcVal.Field(i).IsZero(),
			"nonZeroBuilder did not populate Builder.%s — add it so the Clone check exercises it", name)
	}

	clone := src.Clone()
	require.NotNil(t, clone)

	// Every field in the clone must be non-zero, i.e. Clone preserved it.
	cloneVal := reflect.ValueOf(clone).Elem()
	for i := 0; i < cloneVal.NumField(); i++ {
		name := srcType.Field(i).Name
		assert.False(t, cloneVal.Field(i).IsZero(),
			"Builder.Clone dropped %q — update Clone to copy it", name)
	}

	// Stronger checks where the contract is "deep copy".
	assert.NotSame(t, src.tlsConfig, clone.tlsConfig, "tlsConfig should be deep-cloned (distinct pointer)")

	// Mutate one element of each cloned slice on the source; clone elements must
	// be unaffected. (slices.Clone / bytes.Clone produce a fresh outer slice.)
	src.middlewares[0] = nil
	src.innerMiddlewares[0] = nil
	src.metricsTagProviders[0] = nil
	src.caByteSlices[0] = nil
	src.clientCertKey[0] = 0
	src.clientCertCert[0] = 0
	src.errs[0] = nil

	assert.NotNil(t, clone.middlewares[0], "middlewares slice must be deep-cloned")
	assert.NotNil(t, clone.innerMiddlewares[0], "innerMiddlewares slice must be deep-cloned")
	assert.NotNil(t, clone.metricsTagProviders[0], "metricsTagProviders slice must be deep-cloned")
	assert.NotNil(t, clone.caByteSlices[0], "caByteSlices outer slice must be deep-cloned")
	assert.NotZero(t, clone.clientCertKey[0], "clientCertKey must be deep-cloned")
	assert.NotZero(t, clone.clientCertCert[0], "clientCertCert must be deep-cloned")
	assert.NotNil(t, clone.errs[0], "errs slice must be deep-cloned")
}