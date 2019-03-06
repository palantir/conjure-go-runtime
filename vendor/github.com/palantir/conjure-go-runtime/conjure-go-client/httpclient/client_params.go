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

package httpclient

import (
	"context"
	"crypto/tls"
	"net/http"
	"time"

	"github.com/palantir/pkg/bytesbuffers"
	"github.com/palantir/pkg/retry"
	"github.com/palantir/witchcraft-go-error"
)

// ClientParam is a param that can be used to build
type ClientParam interface {
	apply(builder *clientBuilder) error
}

type HTTPClientParam interface {
	applyHTTPClient(builder *httpClientBuilder) error
}

// ClientOrHTTPClientParam is a param that can be used to build a Client or an http.Client
type ClientOrHTTPClientParam interface {
	ClientParam
	HTTPClientParam
}

// clientParamFunc is a convenience type that helps build a ClientParam. Use when you want a param that can be used to
// build a Client and *not* an http.Client
type clientParamFunc func(builder *clientBuilder) error

func (f clientParamFunc) apply(b *clientBuilder) error {
	return f(b)
}

// httpClientParamFunc is a convenience type that helps build a HTTPClientParam. Use when you want a param that can be used to
// build an http.Client and *not* a Client
type httpClientParamFunc func(builder *httpClientBuilder) error

func (f httpClientParamFunc) applyHTTPClient(b *httpClientBuilder) error {
	return f(b)
}

// clientOrHTTPClientParamFunc is a convenience type that helps build a ClientOrHTTPClientParam. Use when you want a param that can be used to
// either as an Client or a http.Client
type clientOrHTTPClientParamFunc func(builder *httpClientBuilder) error

func (f clientOrHTTPClientParamFunc) apply(b *clientBuilder) error {
	return f(&b.httpClientBuilder)
}

func (f clientOrHTTPClientParamFunc) applyHTTPClient(b *httpClientBuilder) error {
	return f(b)
}

func WithConfig(c ClientConfig) ClientParam {
	return clientParamFunc(func(b *clientBuilder) error {
		params, err := configToParams(c)
		if err != nil {
			return err
		}
		for _, p := range params {
			if err := p.apply(b); err != nil {
				return err
			}
		}
		return nil
	})
}

func WithConfigForHTTPClient(c ClientConfig) HTTPClientParam {
	return httpClientParamFunc(func(b *httpClientBuilder) error {
		params, err := configToParams(c)
		if err != nil {
			return err
		}
		for _, p := range params {
			httpClientParam, ok := p.(HTTPClientParam)
			if !ok {
				return werror.Error("param from config was not a http client builder param")
			}
			if err := httpClientParam.applyHTTPClient(b); err != nil {
				return err
			}
		}
		return nil
	})
}

func WithServiceName(serviceName string) ClientOrHTTPClientParam {
	return clientOrHTTPClientParamFunc(func(b *httpClientBuilder) error {
		b.ServiceName = serviceName
		return nil
	})
}

// WithMiddleware will be invoked for custom HTTP behavior after the
// underlying transport is initialized. Each handler added "wraps" the previous
// round trip, so it will see the request first and the response last.
func WithMiddleware(h Middleware) ClientOrHTTPClientParam {
	return clientOrHTTPClientParamFunc(func(b *httpClientBuilder) error {
		b.Middlewares = append(b.Middlewares, h)
		return nil
	})
}

func WithAddHeader(key, value string) ClientOrHTTPClientParam {
	return WithMiddleware(MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		req.Header.Add(key, value)
		return next.RoundTrip(req)
	}))
}

func WithSetHeader(key, value string) ClientOrHTTPClientParam {
	return WithMiddleware(MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		req.Header.Set(key, value)
		return next.RoundTrip(req)
	}))
}

// WithAuthToken sets the Authorization header to a static bearerToken.
func WithAuthToken(bearerToken string) ClientOrHTTPClientParam {
	return WithAuthTokenProvider(func(context.Context) (string, error) {
		return bearerToken, nil
	})
}

// WithAuthTokenProvider calls provideToken() and sets the Authorization header.
func WithAuthTokenProvider(provideToken TokenProvider) ClientOrHTTPClientParam {
	return WithMiddleware(&authTokenMiddleware{provideToken: provideToken})
}

// WithUserAgent sets the User-Agent header.
func WithUserAgent(userAgent string) ClientOrHTTPClientParam {
	return WithSetHeader("User-Agent", userAgent)
}

// WithMetrics enables the "client.response" metric. See MetricsMiddleware for details.
// The serviceName will appear as the "service-name" tag.
func WithMetrics(tagProviders ...TagsProvider) ClientOrHTTPClientParam {
	return clientOrHTTPClientParamFunc(func(b *httpClientBuilder) error {
		m, err := MetricsMiddleware(b.ServiceName, tagProviders...)
		if err != nil {
			return err
		}
		b.Middlewares = append(b.Middlewares, m)
		return nil
	})
}

// WithBytesBufferPool stores a bytes buffer pool on the client for use in encoding request bodies.
// This prevents allocating a new byte buffer for every request.
func WithBytesBufferPool(pool bytesbuffers.Pool) ClientOrHTTPClientParam {
	return clientOrHTTPClientParamFunc(func(b *httpClientBuilder) error {
		b.BytesBufferPool = pool
		return nil
	})
}

// WithDisablePanicRecovery disables the enabled-by-default panic recovery middleware.
// If the request was otherwise succeeding (err == nil), we return a new zerror with
// the recovered object as an unsafe param. If there's an error, we werror.Wrap it.
// If errMiddleware is not nil, it is invoked on the recovered object.
func WithDisablePanicRecovery() ClientOrHTTPClientParam {
	return clientOrHTTPClientParamFunc(func(b *httpClientBuilder) error {
		b.DisableRecovery = true
		return nil
	})
}

// WithDisableTracing disables the enabled-by-default tracing middleware which
// instructs the client to propagate trace information using the go-zipkin libraries
// method of attaching traces to requests. The server at the other end of such a request, should
// be instrumented to read zipkin-style headers
//
// If a trace is already attached to a request context, then the trace is continued. Otherwise, no
// trace information is propagate. This will not create a span if one does not exist.
func WithDisableTracing() ClientOrHTTPClientParam {
	return clientOrHTTPClientParamFunc(func(b *httpClientBuilder) error {
		b.DisableTracing = true
		return nil
	})
}

// WithDisableTraceHeaderPropagation disables the enabled-by-default traceId header propagation
// By default, if witchcraft-logging has attached a traceId to the context of the request (for service and request logging),
// then the client will attach this traceId as a header for future services to do the same if desired
func WithDisableTraceHeaderPropagation() ClientParam {
	return clientParamFunc(func(b *clientBuilder) error {
		b.disableTraceHeaderPropagation = true
		return nil
	})
}

// WithHTTPTimeout sets the timeout on the http client.
// If unset, the client defaults to 1 minute.
func WithHTTPTimeout(timeout time.Duration) ClientOrHTTPClientParam {
	return clientOrHTTPClientParamFunc(func(b *httpClientBuilder) error {
		b.Timeout = timeout
		return nil
	})
}

// WithDisableHTTP2 skips the default behavior of configuring
// the transport with http2.ConfigureTransport.
func WithDisableHTTP2() ClientOrHTTPClientParam {
	return clientOrHTTPClientParamFunc(func(b *httpClientBuilder) error {
		b.DisableHTTP2 = true
		return nil
	})
}

// WithMaxIdleConns sets the number of reusable TCP connections the client
// will maintain. If unset, the client defaults to 32.
func WithMaxIdleConns(conns int) ClientOrHTTPClientParam {
	return clientOrHTTPClientParamFunc(func(b *httpClientBuilder) error {
		b.MaxIdleConns = conns
		return nil
	})
}

// WithMaxIdleConnsPerHost sets the number of reusable TCP connections the client
// will maintain per destination. If unset, the client defaults to 32.
func WithMaxIdleConnsPerHost(conns int) ClientOrHTTPClientParam {
	return clientOrHTTPClientParamFunc(func(b *httpClientBuilder) error {
		b.MaxIdleConnsPerHost = conns
		return nil
	})
}

// WithNoProxy nils out the Proxy field of the http.Transport,
// ignoring any proxy set in the process's environment.
// If unset, the default is http.ProxyFromEnvironment.
func WithNoProxy() ClientOrHTTPClientParam {
	return clientOrHTTPClientParamFunc(func(b *httpClientBuilder) error {
		b.Proxy = nil
		return nil
	})
}

// WithProxyFromEnvironment can be used to set the HTTP(s) proxy to use
// the Go standard library's http.ProxyFromEnvironment.
func WithProxyFromEnvironment() ClientOrHTTPClientParam {
	return clientOrHTTPClientParamFunc(func(b *httpClientBuilder) error {
		b.Proxy = http.ProxyFromEnvironment
		return nil
	})
}

// WithTLSConfig sets the SSL/TLS configuration for the HTTP client's Transport using a copy of the provided config.
// The palantir/pkg/tlsconfig package is recommended to build a tls.Config from sane defaults.
func WithTLSConfig(conf *tls.Config) ClientOrHTTPClientParam {
	return clientOrHTTPClientParamFunc(func(b *httpClientBuilder) error {
		b.TLSClientConfig = conf.Clone()
		return nil
	})
}

// WithDialTimeout sets the timeout on the Dialer.
// If unset, the client defaults to 30 seconds.
func WithDialTimeout(timeout time.Duration) ClientOrHTTPClientParam {
	return clientOrHTTPClientParamFunc(func(b *httpClientBuilder) error {
		b.DialTimeout = timeout
		return nil
	})
}

// WithKeepAlive sets the keep alive frequency on the Dialer.
// If unset, the client defaults to 30 seconds.
func WithKeepAlive(keepAlive time.Duration) ClientOrHTTPClientParam {
	return clientOrHTTPClientParamFunc(func(b *httpClientBuilder) error {
		b.KeepAlive = keepAlive
		return nil
	})
}

// WithBaseURLs sets the base URLs for every request. This is meant to be used in conjunction with WithPath.
func WithBaseURLs(urls []string) ClientParam {
	return clientParamFunc(func(b *clientBuilder) error {
		b.uris = urls
		return nil
	})
}

// WithMaxBackoff sets the maximum backoff between retried calls to the same URI. Defaults to no limit.
func WithMaxBackoff(maxBackoff time.Duration) ClientParam {
	return clientParamFunc(func(b *clientBuilder) error {
		b.backoffOptions = append(b.backoffOptions, retry.WithMaxBackoff(maxBackoff))
		return nil
	})
}

// WithInitialBackoff sets the initial backoff between retried calls to the same URI. Defaults to 250ms.
func WithInitialBackoff(initialBackoff time.Duration) ClientParam {
	return clientParamFunc(func(b *clientBuilder) error {
		b.backoffOptions = append(b.backoffOptions, retry.WithInitialBackoff(initialBackoff))
		return nil
	})
}

// WithMaxRetries sets the maximum number of retries on transport errors for every request. Backoffs are
// also capped at this.
// If unset, the client defaults to 2 * size of URIs
func WithMaxRetries(maxTransportRetries int) ClientParam {
	return clientParamFunc(func(b *clientBuilder) error {
		b.maxRetries = maxTransportRetries
		return nil
	})
}

// WithDisableRestErrors disables the middleware which sets Do()'s returned
// error to a non-nil value in the case of >= 400 HTTP response.
func WithDisableRestErrors() ClientParam {
	return clientParamFunc(func(b *clientBuilder) error {
		b.errorDecoder = nil
		return nil
	})
}

func WithErrorDecoder(errorDecoder ErrorDecoder) ClientParam {
	return clientParamFunc(func(b *clientBuilder) error {
		b.errorDecoder = errorDecoder
		return nil
	})
}
