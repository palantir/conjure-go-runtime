package httpc

import "time"

// TransportBuilder is an F-bounded interface for configuring HTTP transport settings
// including connection pooling, timeouts, HTTP/2, and proxy configuration.
//
// Like all builders in this package, TransportBuilder is mutable: setter methods modify
// the receiver and return it for chaining. Use Clone to fork an independent copy.
//
// Generic functions can accept any TransportBuilder and return the same concrete type:
//
//	func ConfigureTransport[Self TransportBuilder[Self]](b Self) Self {
//	    return b.SetMaxIdleConnsPerHost(50).SetIdleConnTimeout(60 * time.Second)
//	}
type TransportBuilder[B TransportBuilder[B]] interface {
	// Clone returns a deep copy of the builder. The copy is fully independent:
	// mutations to either the original or the clone do not affect the other.
	Clone() B

	// Apply applies the given Param functions to the builder in sequence.
	// Each Param may call setter methods to configure the builder.
	// Because builders are mutable, this modifies the receiver in place.
	Apply(...Param[B]) B

	// SetMaxIdleConns sets the maximum total number of idle connections across all hosts.
	// Default: 200.
	SetMaxIdleConns(int) B

	// SetMaxIdleConnsPerHost sets the maximum number of idle connections per host.
	// Default: 100.
	SetMaxIdleConnsPerHost(int) B

	// DisableKeepAlives disables HTTP keep-alive connections, forcing a new connection per request.
	DisableKeepAlives() B

	// SetIdleConnTimeout sets how long idle connections remain in the pool before closing.
	// Default: 90s.
	SetIdleConnTimeout(time.Duration) B

	// SetExpectContinueTimeout sets the timeout for waiting for a 100 Continue response.
	// Default: 1s.
	SetExpectContinueTimeout(time.Duration) B

	// SetResponseHeaderTimeout sets the timeout for reading response headers.
	// Default: 0 (no timeout).
	SetResponseHeaderTimeout(time.Duration) B

	// SetTLSHandshakeTimeout sets the timeout for the TLS handshake.
	// Default: 10s.
	SetTLSHandshakeTimeout(time.Duration) B

	// DisableHTTP2 disables HTTP/2 support, forcing HTTP/1.1.
	DisableHTTP2() B

	// SetHTTP2ReadIdleTimeout sets the timeout after which a health check is performed
	// on idle HTTP/2 connections.
	// Default: 30s.
	SetHTTP2ReadIdleTimeout(time.Duration) B

	// SetHTTP2PingTimeout sets the timeout for HTTP/2 ping health checks.
	// Default: 15s.
	SetHTTP2PingTimeout(time.Duration) B

	// SetHTTPProxyURL sets an HTTP/HTTPS proxy URL for requests.
	// Pass "" to clear. SOCKS proxy configuration is on DialerBuilder.
	SetHTTPProxyURL(string) B

	// SetNoProxy clears all proxy configuration.
	SetNoProxy() B

	// SetProxyFromEnvironment configures the proxy from environment variables
	// (HTTP_PROXY, HTTPS_PROXY, NO_PROXY).
	SetProxyFromEnvironment() B
}
