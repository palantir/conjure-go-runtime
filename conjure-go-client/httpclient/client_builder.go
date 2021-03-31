// Copyright (c) 2018 Palantir Technologies. All rights reserved.
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

// Package httpclient provides round trippers/transport wrappers for http clients.
package httpclient

import (
	"context"
	"net/http"
	"time"

	"github.com/palantir/pkg/bytesbuffers"
	"github.com/palantir/pkg/refreshable"
	"github.com/palantir/pkg/retry"
	"github.com/palantir/pkg/tlsconfig"
)

type clientBuilder struct {
	httpClientBuilder

	uris                   refreshable.StringSlice
	maxAttempts            int
	enableUnlimitedRetries bool
	backoffOptions         []retry.Option

	errorDecoder                  ErrorDecoder
	disableTraceHeaderPropagation bool
}

type httpClientBuilder struct {
	ServiceName     string
	Timeout         refreshable.Duration
	TransportParams refreshableClientParams
	Middlewares     []Middleware
	BytesBufferPool bytesbuffers.Pool
}

// NewClient returns a configured client ready for use.
// We apply "sane defaults" before applying the provided params.
func NewClient(params ...ClientParam) (Client, error) {
	defaultTLSConfig, _ := tlsconfig.NewClientConfig()
	b := &clientBuilder{
		httpClientBuilder: httpClientBuilder{
			TransportParams: refreshableClientParams{
				Transport: refreshableTransportParams{
					TLSClientConfig: refreshable.NewDefaultRefreshable(defaultTLSConfig),
				},
			},
		},
		backoffOptions: []retry.Option{retry.WithInitialBackoff(250 * time.Millisecond)},
		errorDecoder:   restErrorDecoder{},
	}
	for _, p := range params {
		if p == nil {
			continue
		}
		if err := p.apply(b); err != nil {
			return nil, err
		}
	}
	provider, err := newRefreshableHTTPClient(context.TODO(), b.TransportParams)
	if err != nil {
		return nil, err
	}

	var edm Middleware
	if b.errorDecoder != nil {
		edm = errorDecoderMiddleware(b.errorDecoder)
	}

	if b.enableUnlimitedRetries {
		// max retries of 0 indicates no limit
		b.maxAttempts = 0
	} else if b.maxAttempts == 0 {
		if b.uris != nil {
			b.maxAttempts = 2 * len(b.uris.CurrentStringSlice())
		}
	}

	return &clientImpl{
		client:                        provider,
		uris:                          b.uris,
		maxAttempts:                   b.maxAttempts,
		backoffOptions:                b.backoffOptions,
		disableTraceHeaderPropagation: b.disableTraceHeaderPropagation,
		middlewares:                   b.Middlewares,
		errorDecoderMiddleware:        edm,
		bufferPool:                    b.BytesBufferPool,
	}, nil
}

// NewHTTPClient returns a configured http client ready for use.
// We apply "sane defaults" before applying the provided params.
func NewHTTPClient(params ...HTTPClientParam) (*http.Client, error) {
	defaultTLSConfig, _ := tlsconfig.NewClientConfig()
	b := &httpClientBuilder{
		TransportParams: refreshableClientParams{
			Transport: refreshableTransportParams{
				TLSClientConfig: refreshable.NewDefaultRefreshable(defaultTLSConfig),
			},
		},
	}
	for _, p := range params {
		if p == nil {
			continue
		}
		if err := p.applyHTTPClient(b); err != nil {
			return nil, err
		}
	}
	provider, err := newRefreshableHTTPClient(context.TODO(), b.TransportParams)
	if err != nil {
		return nil, err
	}
	return provider.CurrentHTTPClient(), nil
}
