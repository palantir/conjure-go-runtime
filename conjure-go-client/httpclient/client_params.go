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
	"net/url"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/palantir/pkg/bytesbuffers"
	"github.com/palantir/pkg/refreshable/v2"
	werror "github.com/palantir/witchcraft-go-error"
)

// ClientParam is a param that can be used to build
type ClientParam interface {
	apply(builder *clientBuilder) error
}

type HTTPClientParam interface {
	applyHTTPClient(builder *httpc.Builder) error
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

// clientOrHTTPClientParamFunc is a convenience type that helps build a ClientOrHTTPClientParam. Use when you want a param that can be used to
// either as an Client or a http.Client
type clientOrHTTPClientParamFunc func(builder *httpc.Builder) error

func (f clientOrHTTPClientParamFunc) apply(b *clientBuilder) error {
	return f(b.HTTP)
}

func (f clientOrHTTPClientParamFunc) applyHTTPClient(b *httpc.Builder) error {
	return f(b)
}

// builderClientOrHTTPClientParam wraps an httpc.Param to implement both ClientParam and HTTPClientParam
// by delegating to the underlying httpc.Builder.
type builderClientOrHTTPClientParam httpc.Param[*httpc.Builder]

func (f builderClientOrHTTPClientParam) apply(b *clientBuilder) error {
	return f.applyHTTPClient(b.HTTP)
}

func (f builderClientOrHTTPClientParam) applyHTTPClient(b *httpc.Builder) error {
	b.Apply(httpc.Param[*httpc.Builder](f))
	return nil
}

func WithConfig(c ClientConfig) ClientParam {
	return builderClientOrHTTPClientParam(func(b *httpc.Builder) *httpc.Builder {
		return b.ApplyConfig(context.Background(), c)
	})
}

func WithConfigForHTTPClient(c ClientConfig) HTTPClientParam {
	return builderClientOrHTTPClientParam(func(b *httpc.Builder) *httpc.Builder {
		return b.ApplyConfig(context.Background(), c)
	})
}

func WithServiceName(serviceName string) ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetServiceName, serviceName))
}

// WithMiddleware will be invoked for custom HTTP behavior after the
// underlying transport is initialized. Each handler added "wraps" the previous
// round trip, so it will see the request first and the response last.
func WithMiddleware(h Middleware) ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).AddMiddleware, h))
}

// WithTLSCABytes sets the root CA certificates for the HTTP client's TLS config using a refreshable
// source of PEM-encoded bytes. The TLS configuration will be rebuilt whenever the refreshable updates.
// This is useful when the CA certificates are available in memory rather than on disk and may change over time.
func WithTLSCABytes(caBytes refreshable.Refreshable[[][]byte]) ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).AddCACertBytesRefreshable, caBytes))
}

// WithInnerMiddleware is like WithMiddleware, but adds the handler to the
// beginning of the middleware chain. This function will see the request last
// (after all already-configured middleware) and see the response first.
// This is useful for middleware that wants to mutate the request, including
// overwriting actions from previous middleware.
func WithInnerMiddleware(h Middleware) ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).AddInnerMiddleware, h))
}

func WithAddHeader(key, value string) ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(func(b *httpc.Builder) *httpc.Builder {
		return b.AddHeader(key, value)
	})
}

func WithSetHeader(key, value string) ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(func(b *httpc.Builder) *httpc.Builder {
		return b.SetHeader(key, value)
	})
}

// WithAuthToken sets the Authorization header to a static bearerToken.
func WithAuthToken(bearerToken string) ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetAuthToken, bearerToken))
}

// WithAuthTokenProvider calls provideToken() and sets the Authorization header.
func WithAuthTokenProvider(provideToken TokenProvider) ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetAuthTokenProvider, httpc.TokenProvider(provideToken)))
}

// WithUserAgent sets the User-Agent header.
func WithUserAgent(userAgent string) ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetUserAgent, userAgent))
}

// WithOverrideRequestHost overrides the request Host from the default URL.Host
func WithOverrideRequestHost(host string) ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetOverrideRequestHost, host))
}

// WithMetrics enables the "client.response" metric. See MetricsMiddleware for details.
// The serviceName will appear as the "service-name" tag.
func WithMetrics(tagProviders ...TagsProvider) ClientOrHTTPClientParam {
	// Slice conversion required: TagsProvider → httpc.TagsProvider.
	httpcProviders := make([]httpc.TagsProvider, len(tagProviders))
	for i, tp := range tagProviders {
		httpcProviders[i] = tp
	}
	return builderClientOrHTTPClientParam(httpc.ParamVarArgs((*httpc.Builder).SetMetrics, httpcProviders))
}

// WithoutMetrics disables the "client.response" metric.
// Also clears any MetricsTagProviders that were set
func WithoutMetrics() ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetDisableMetrics, true))
}

// WithBytesBufferPool stores a bytes buffer pool on the client for use in encoding request bodies.
// This prevents allocating a new byte buffer for every request.
func WithBytesBufferPool(pool bytesbuffers.Pool) ClientParam {
	return clientParamFunc(func(b *clientBuilder) error {
		b.BytesBufferPool = pool
		return nil
	})
}

// WithDisablePanicRecovery disables the enabled-by-default panic recovery middleware.
// If the request was otherwise succeeding (err == nil), we return a new werror with
// the recovered object as an unsafe param. If there's an error, we werror.Wrap it.
// If errMiddleware is not nil, it is invoked on the recovered object.
func WithDisablePanicRecovery() ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param0((*httpc.Builder).DisablePanicRecovery))
}

// WithDisableTracing disables the enabled-by-default tracing middleware which
// instructs the client to propagate trace information using the go-zipkin libraries
// method of attaching traces to requests. The server at the other end of such a request should
// be instrumented to read zipkin-style headers
//
// If a trace is already attached to a request context, then the trace is continued. Otherwise, no
// trace information is propagate. This will not create a span if one does not exist.
func WithDisableTracing() ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param0((*httpc.Builder).DisableTracing))
}

// WithDisableTraceHeaderPropagation disables the enabled-by-default traceId header propagation
// By default, if witchcraft-logging has attached a traceId to the context of the request (for service and request logging),
// then the client will attach this traceId as a header for future services to do the same if desired
func WithDisableTraceHeaderPropagation() ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param0((*httpc.Builder).DisableTraceHeaderPropagation))
}

// WithHTTPTimeout sets the timeout on the http client.
// If unset, the client defaults to 1 minute.
func WithHTTPTimeout(timeout time.Duration) ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetTimeout, timeout))
}

// WithDisableHTTP2 skips the default behavior of configuring
// the transport with http2.ConfigureTransport.
func WithDisableHTTP2() ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param0((*httpc.Builder).DisableHTTP2))
}

// WithHTTP2ReadIdleTimeout configures the HTTP/2 ReadIdleTimeout.
// A ReadIdleTimeout > 0 will enable health checks and allows broken/idle
// connections to be pruned more quickly, preventing the client from
// attempting to re-use connections that will no longer work.
// If the HTTP/2 connection has not received any frames after the ReadIdleTimeout period,
// then periodic pings (health checks) will be sent to the server before attempting to close the connection.
// The amount of time to wait for the ping response can be configured by the WithHTTP2PingTimeout param.
// If unset, the client defaults to 30 seconds, if HTTP2 is enabled.
func WithHTTP2ReadIdleTimeout(timeout time.Duration) ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetHTTP2ReadIdleTimeout, timeout))
}

// WithHTTP2PingTimeout configures the amount of time to wait for a ping response
// before closing an HTTP/2 connection. The PingTimeout is only valid when
// the ReadIdleTimeout is > 0 otherwise pings (health checks) are not enabled.
// If unset, the client defaults to 15 seconds, if HTTP/2 is enabled and the ReadIdleTimeout is > 0.
func WithHTTP2PingTimeout(timeout time.Duration) ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetHTTP2PingTimeout, timeout))
}

// WithMaxIdleConns sets the number of reusable TCP connections the client
// will maintain. If unset, the client defaults to 200.
func WithMaxIdleConns(conns int) ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetMaxIdleConns, conns))
}

// WithMaxIdleConnsPerHost sets the number of reusable TCP connections the client
// will maintain per destination. If unset, the client defaults to 100.
func WithMaxIdleConnsPerHost(conns int) ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetMaxIdleConnsPerHost, conns))
}

// WithNoProxy nils out the Proxy field of the http.Transport,
// ignoring any proxy set in the process's environment.
// If unset, the default is http.ProxyFromEnvironment.
func WithNoProxy() ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param0((*httpc.Builder).SetNoProxy))
}

// WithProxyFromEnvironment can be used to set the HTTP(s) proxy to use
// the Go standard library's http.ProxyFromEnvironment.
func WithProxyFromEnvironment() ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param0((*httpc.Builder).SetProxyFromEnvironment))
}

// WithProxyURL can be used to set a socks5 or HTTP(s) proxy.
func WithProxyURL(proxyURLString string) ClientOrHTTPClientParam {
	return clientOrHTTPClientParamFunc(func(b *httpc.Builder) error {
		proxyURL, err := url.Parse(proxyURLString)
		if err != nil {
			return werror.Wrap(err, "failed to parse proxy url")
		}
		switch proxyURL.Scheme {
		case "http", "https":
			b.SetHTTPProxyURL(proxyURL.String())
		case "socks5", "socks5h":
			b.SetSocksProxyURL(proxyURL.String())
		default:
			return werror.Error("unrecognized proxy scheme", werror.SafeParam("scheme", proxyURL.Scheme))
		}
		return nil
	})
}

// WithTLSConfig sets the SSL/TLS configuration for the HTTP client's Transport using a copy of the provided config.
// The palantir/pkg/tlsconfig package is recommended to build a tls.Config from sane defaults.
func WithTLSConfig(conf *tls.Config) ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetTLSConfig, conf))
}

// WithTLSInsecureSkipVerify sets the InsecureSkipVerify field for the HTTP client's tls config.
// This option should only be used in clients that have way to establish trust with servers.
// If WithTLSConfig is used, the config's InsecureSkipVerify is set to true.
func WithTLSInsecureSkipVerify() ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetInsecureSkipVerify, true))
}

// WithKeyAndCertFile sets the client TLS certificate and key file paths.
// The files are read when the client is created and on each request to support certificate rotation.
func WithKeyAndCertFile(keyFile string, certFile string) ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param2((*httpc.Builder).SetClientCertFiles, certFile, keyFile))
}

// WithDynamicCertReload enables re-reading client TLS cert/key files on each TLS handshake.
// By default, client certificates are loaded once at client creation time. When this option is
// enabled, the cert and key files are re-read from disk on every TLS handshake, allowing the
// client to pick up rotated certificates without restarting.
func WithDynamicCertReload() ClientOrHTTPClientParam {
	return clientOrHTTPClientParamFunc(func(b *httpc.Builder) error {
		b.SetDynamicCertReload(true)
		return nil
	})
}

// WithCAFiles sets the CA certificate file paths for the client's TLS configuration.
// The files are read when the client is created and periodically refreshed to support certificate rotation.
func WithCAFiles(CAFiles []string) ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.ParamVarArgs((*httpc.Builder).AddCACertFiles, CAFiles))
}

// WithDialTimeout sets the timeout on the Dialer.
// If unset, the client defaults to 90 seconds.
func WithDialTimeout(timeout time.Duration) ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetDialTimeout, timeout))
}

// WithIdleConnTimeout sets the timeout for idle connections.
// If unset, the client defaults to 90 seconds.
func WithIdleConnTimeout(timeout time.Duration) ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetIdleConnTimeout, timeout))
}

// WithTLSHandshakeTimeout sets the timeout for TLS handshakes.
// If unset, the client defaults to 10 seconds.
func WithTLSHandshakeTimeout(timeout time.Duration) ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetTLSHandshakeTimeout, timeout))
}

// WithExpectContinueTimeout sets the timeout to receive the server's first response headers after
// fully writing the request headers if the request has an "Expect: 100-continue" header.
// If unset, the client defaults to 1 second.
func WithExpectContinueTimeout(timeout time.Duration) ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetExpectContinueTimeout, timeout))
}

// WithResponseHeaderTimeout specifies the amount of time to wait for a server's response headers after fully writing
// the request (including its body, if any). This time does not include the time to read the response body. If unset,
// the client defaults to having no response header timeout.
func WithResponseHeaderTimeout(timeout time.Duration) ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetResponseHeaderTimeout, timeout))
}

// WithKeepAlive sets the keep alive frequency on the Dialer.
// If unset, the client defaults to 30 seconds.
func WithKeepAlive(keepAlive time.Duration) ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetKeepAlive, keepAlive))
}

// WithBaseURLs sets the base URLs for every request. This is meant to be used in conjunction with WithPath.
func WithBaseURLs(urls []string) ClientParam {
	return builderClientOrHTTPClientParam(httpc.ParamVarArgs((*httpc.Builder).SetBaseURLs, urls))
}

// WithRefreshableBaseURLs sets the base URLs for every request. This is meant to be used in conjunction with WithPath.
func WithRefreshableBaseURLs(urls refreshable.Refreshable[[]string]) ClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetBaseURLsRefreshable, urls))
}

// WithAllowCreateWithEmptyURIs prevents NewClient from returning an error when the URI slice is empty.
// This is useful when the URIs are not known at client creation time but will be populated by a refreshable.
// Requests will error if attempted before URIs are populated.
func WithAllowCreateWithEmptyURIs() ClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetAllowCreateWithEmptyURIs, true))
}

// WithMaxBackoff sets the maximum backoff between retried calls to the same URI.
// Defaults to 2 seconds. <= 0 indicates no limit.
func WithMaxBackoff(maxBackoff time.Duration) ClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetMaxBackoff, maxBackoff))
}

// WithInitialBackoff sets the initial backoff between retried calls to the same URI. Defaults to 250ms.
func WithInitialBackoff(initialBackoff time.Duration) ClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetInitialBackoff, initialBackoff))
}

// WithMaxRetries sets the maximum number of retries on transport errors for every request. Backoffs are
// also capped at this.
// If unset, the client defaults to 2 * size of URIs
// TODO (#151): Rename to WithMaxAttempts and set maxAttempts directly using the argument provided to the function.
func WithMaxRetries(maxTransportRetries int) ClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetMaxAttempts, new(maxTransportRetries+1)))
}

// WithUnlimitedRetries sets an unlimited number of retries on transport errors for every request.
// If set, this supersedes any retry limits set with WithMaxRetries.
func WithUnlimitedRetries() ClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetMaxAttempts, new(int)))
}

// WithDisableRestErrors disables the middleware which sets Do()'s returned
// error to a non-nil value in the case of >= 400 HTTP response.
func WithDisableRestErrors() ClientParam {
	return clientParamFunc(func(b *clientBuilder) error {
		b.ErrorDecoder = nil
		return nil
	})
}

// WithDisableKeepAlives disables keep alives on the http transport
func WithDisableKeepAlives() ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param0((*httpc.Builder).DisableKeepAlives))
}

func WithErrorDecoder(errorDecoder ErrorDecoder) ClientParam {
	return clientParamFunc(func(b *clientBuilder) error {
		b.ErrorDecoder = errorDecoder
		return nil
	})
}

// WithBasicAuth sets the request's Authorization header to use HTTP Basic Authentication with the provided username and
// password.
func WithBasicAuth(user, password string) ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param2((*httpc.Builder).SetBasicAuth, user, password))
}

// WithBasicAuthProvider sets the request's Authorization header to use HTTP Basic Authentication.
// The provider is expected to always return a nonempty BasicAuth value, or an error.
func WithBasicAuthProvider(provider BasicAuthProvider) ClientOrHTTPClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetBasicAuthProvider, httpc.BasicAuthProvider(provider)))
}

// WithBasicAuthOptionalProvider sets the request's Authorization header to use HTTP Basic Authentication based on the
// return value of the provided BasicAuthOptionalProvider. If the provider returns a non-nil error, if the returned
// BasicAuth value is non-nil then its values are set on the header, while if the returned BasicAuth value is nil then
// no basic authentication header values are set.
func WithBasicAuthOptionalProvider(provider BasicAuthOptionalProvider) ClientOrHTTPClientParam {
	return WithInnerMiddleware(MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		basicAuth, err := provider(req.Context())
		if err != nil {
			return nil, err
		}
		if basicAuth != nil {
			setBasicAuth(req.Header, basicAuth.User, basicAuth.Password)
		}
		return next.RoundTrip(req)
	}))
}

// WithRandomURIScoring adds middleware that randomizes the order URIs are prioritized in for each request.
func WithRandomURIScoring() ClientParam {
	return builderClientOrHTTPClientParam(httpc.Param1((*httpc.Builder).SetURIScoringStrategy, httpc.URIScoringRandom))
}
