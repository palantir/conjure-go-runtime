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
	"context"
	"net/http"
	"slices"
	"sync"

	werror "github.com/palantir/witchcraft-go-error"
)

var (
	// ErrStickySessionUnsupported is returned by NewStickySession when a Client does not support sticky sessions.
	ErrStickySessionUnsupported = werror.Error("httpclient: client does not support sticky sessions")

	// ErrStickyPinInvalidated is returned by a sticky session's methods once its pinned URI is no longer part of the current URI set.
	// A sticky session never repins itself to a different URI. Once invalidated it fails every subsequent call, so callers should establish a
	// new sticky session to continue.
	ErrStickyPinInvalidated = werror.Error("httpclient: sticky session's pinned URI is no longer part of the client's URI set")
)

type stickySessionProvider interface {
	newStickySession() (Client, error)
}

// NewStickySession returns a Client that pins every request it sends to a single URI for its entire
// lifetime. Returns an ErrStickySessionUnsupported error if the provided Client does not support sticky sessions.
//
// The pin is established from the outcome of the session's first request. The first request is sent
// through the Client's normal load-balanced, multi-attempt path, exactly like a non-sticky call, so it
// benefits from the usual cross-host failover. If it succeeds (a 2xx response), the URI that served
// it becomes the pin. Every call after that makes exactly one attempt against the pinned URI and it is
// never retried and never fails over to a different URI.
//
// On each call after a pin is established, if the pinned URI was one of the client's configured URIs, it is
// checked against the client's current URI set. If it is no longer present, the call fails immediately with
// ErrStickyPinInvalidated rather than silently repinning.
func NewStickySession(c Client) (Client, error) {
	provider, ok := c.(stickySessionProvider)
	if !ok {
		return nil, ErrStickySessionUnsupported
	}
	return provider.newStickySession()
}

func (c *clientImpl) newStickySession() (Client, error) {
	return &stickyClient{
		parent: c,
	}, nil
}

var _ Client = (*stickyClient)(nil)

// stickyClient is the Client returned by NewStickySession.
type stickyClient struct {
	parent *clientImpl

	mu        sync.Mutex
	pinnedURI string // "" until a pin is established

	// pinnedURIIsListed records whether pinnedURI was one of the client's configured URIs at the
	// moment it was captured, as opposed to a URI reached only via a 307/308 redirect.
	pinnedURIIsListed bool
}

func (s *stickyClient) Do(ctx context.Context, params ...RequestParam) (*http.Response, error) {
	if p, ok := s.currentPin(); ok {
		return s.doWithPin(ctx, p, params...)
	}
	return s.doAndMaybePin(ctx, params...)
}

type pin struct {
	uri      string
	isListed bool
}

func (s *stickyClient) currentPin() (pin, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return pin{
		uri:      s.pinnedURI,
		isListed: s.pinnedURIIsListed,
	}, s.pinnedURI != ""
}

// doAndMaybePin sends the request through the parent's normal load-balanced path, since no pin exists
// yet, and if it succeeds, establishes the pin as the URI that served the response. If another
// concurrent call has already established a pin by the time this one succeeds, that earlier pin wins.
func (s *stickyClient) doAndMaybePin(ctx context.Context, params ...RequestParam) (*http.Response, error) {
	uris, attempts := s.parent.currentURIsAndMaxAttempts()
	if len(uris) == 0 {
		return nil, werror.WrapWithContextParams(ctx, ErrEmptyURIs, "", werror.SafeParam("serviceName", s.parent.serviceName.Current()))
	}
	resp, succeededURI, err := s.parent.doWithURIs(ctx, uris, attempts, params...)
	if succeededURI != "" {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.pinnedURI == "" {
			s.pinnedURI = succeededURI
			s.pinnedURIIsListed = slices.Contains(uris, succeededURI)
		}
	}
	return resp, err
}

// doWithPin makes exactly one attempt against the pinned URI. It never retries and never fails over
// to a different URI, even on error.
func (s *stickyClient) doWithPin(ctx context.Context, p pin, params ...RequestParam) (*http.Response, error) {
	if p.isListed && !s.pinIsCurrent(p.uri) {
		return nil, werror.WrapWithContextParams(ctx, ErrStickyPinInvalidated, "invalid URI for request",
			werror.SafeParam("serviceName", s.parent.serviceName.Current()))
	}
	resp, _, err := s.parent.doWithURIs(ctx, []string{p.uri}, 1, params...)
	return resp, err
}

func (s *stickyClient) pinIsCurrent(pinnedURI string) bool {
	uris := s.parent.uriScorer.CurrentURIScoringMiddleware().GetURIsInOrderOfIncreasingScore()
	return slices.Contains(uris, pinnedURI)
}

func (s *stickyClient) Get(ctx context.Context, params ...RequestParam) (*http.Response, error) {
	return s.Do(ctx, append(params, WithRequestMethod(http.MethodGet))...)
}

func (s *stickyClient) Head(ctx context.Context, params ...RequestParam) (*http.Response, error) {
	return s.Do(ctx, append(params, WithRequestMethod(http.MethodHead))...)
}

func (s *stickyClient) Post(ctx context.Context, params ...RequestParam) (*http.Response, error) {
	return s.Do(ctx, append(params, WithRequestMethod(http.MethodPost))...)
}

func (s *stickyClient) Put(ctx context.Context, params ...RequestParam) (*http.Response, error) {
	return s.Do(ctx, append(params, WithRequestMethod(http.MethodPut))...)
}

func (s *stickyClient) Delete(ctx context.Context, params ...RequestParam) (*http.Response, error) {
	return s.Do(ctx, append(params, WithRequestMethod(http.MethodDelete))...)
}
