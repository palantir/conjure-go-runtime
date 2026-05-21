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
	"runtime"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/palantir/pkg/bytesbuffers"
	"github.com/palantir/pkg/refreshable/v2"
)

var (
	// ErrEmptyURIs is returned when the client expects to have base URIs configured to make requests, but the URIs are empty.
	// This check occurs in two places: when the client is constructed and when a request is executed.
	// To avoid the construction validation, use WithAllowCreateWithEmptyURIs().
	ErrEmptyURIs = httpc.ErrEmptyURIs{}
)

type clientBuilder struct {
	HTTP *httpc.Builder

	ErrorDecoder    ErrorDecoder // wrapper handles error decoding
	BytesBufferPool bytesbuffers.Pool
}

func newClientBuilder() *clientBuilder {
	return &clientBuilder{
		HTTP:         httpc.NewBuilder(),
		ErrorDecoder: httpc.DefaultErrorDecoder(),
	}
}

func newClient(ctx context.Context, b *clientBuilder, params ...ClientParam) (*clientImpl, error) {
	for _, p := range params {
		if p != nil {
			if err := p.apply(b); err != nil {
				return nil, err
			}
		}
	}

	// httpc returns raw responses (no error decoding); error decoding is applied
	// by clientImpl.Do after the retry loop completes.
	b.HTTP.DisableRestErrors()

	if b.BytesBufferPool != nil {
		b.HTTP.SetBytesBufferPool(b.BytesBufferPool)
	}

	client, err := b.HTTP.Build(ctx)
	if err != nil {
		return nil, err
	}

	return &clientImpl{
		client:       client,
		errorDecoder: b.ErrorDecoder,
		bufferPool:   b.BytesBufferPool,
	}, nil
}

// Deprecated: prefer [NewClientWithContext].
//
// NewClient returns a configured client ready for use.
// The builder used to build this client is provided with a Context that is associated with the lifetime of the returned
// struct that implements Client, and may be canceled when the pointer to the struct is no longer reachable.
func NewClient(params ...ClientParam) (Client, error) {
	ctx, cancelFn := context.WithCancel(context.Background())
	// note: does not delegate to NewClientWithContext because runtime.AddCleanup needs the actual pointer
	client, err := newClient(ctx, newClientBuilder(), params...)
	if client == nil {
		cancelFn()
	} else {
		runtime.AddCleanup(client, func(cancel context.CancelFunc) { cancel() }, cancelFn)
	}
	return client, err
}

// NewClientWithContext returns a configured client ready for use.
// The provided ctx is used to build the client and is provided to any functions in the builder that require a Context.
// If the provided ctx is canceled, background tasks associated with the returned Client may also be canceled and the
// Client may no longer be valid.
// Sane defaults are applied to the builder before applying the provided params.
func NewClientWithContext(ctx context.Context, params ...ClientParam) (Client, error) {
	return newClient(ctx, newClientBuilder(), params...)
}

// NewClientFromRefreshableConfig returns a configured client ready for use.
// We apply "sane defaults" before applying the provided params.
func NewClientFromRefreshableConfig(ctx context.Context, config refreshable.Refreshable[ClientConfig], params ...ClientParam) (Client, error) {
	b := newClientBuilder()
	b.HTTP.ApplyConfigRefreshable(ctx, config)
	return newClient(ctx, b, params...)
}

// NewHTTPClientWithContext returns a configured *http.Client ready for use.
// The provided ctx is used to build the client and is provided to any functions in the builder that require a Context.
// If the provided ctx is canceled, background tasks associated with the returned *http.Client may also be canceled and
// the *http.Client may no longer be valid.
// Sane defaults are applied to the builder before applying the provided params.
func NewHTTPClientWithContext(ctx context.Context, params ...HTTPClientParam) (*http.Client, error) {
	b := httpc.NewBuilder()
	for _, p := range params {
		if p == nil {
			continue
		}
		if err := p.applyHTTPClient(b); err != nil {
			return nil, err
		}
	}
	httpClient, err := b.BuildHTTPClient(ctx)
	if err != nil {
		return nil, err
	}
	return httpClient.Current(), nil
}

// Deprecated: prefer [NewHTTPClientWithContext].
//
// NewHTTPClient returns a configured *http.Client ready for use.
// The builder used to build this client is provided with a Context that is associated with the lifetime of the returned
// *http.Client, and may be canceled when the pointer is no longer reachable.
func NewHTTPClient(params ...HTTPClientParam) (*http.Client, error) {
	ctx, cancelFn := context.WithCancel(context.Background())
	client, err := NewHTTPClientWithContext(ctx, params...)
	if client == nil {
		cancelFn()
	} else {
		runtime.AddCleanup(client, func(cancel context.CancelFunc) { cancel() }, cancelFn)
	}
	return client, err
}

// NewHTTPClientFromRefreshableConfig returns a configured http client ready for use.
// We apply "sane defaults" before applying the provided params.
func NewHTTPClientFromRefreshableConfig(ctx context.Context, config refreshable.Refreshable[ClientConfig], params ...HTTPClientParam) (refreshable.Refreshable[*http.Client], error) {
	b := httpc.NewBuilder()
	b.ApplyConfigRefreshable(ctx, config)
	for _, p := range params {
		if p == nil {
			continue
		}
		if err := p.applyHTTPClient(b); err != nil {
			return nil, err
		}
	}
	return b.BuildHTTPClient(ctx)
}
