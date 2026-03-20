package httpc

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/url"
	"time"

	"github.com/palantir/pkg/refreshable/v2"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/palantir/witchcraft-go-logging/wlog/svclog/svc1log"
	"golang.org/x/net/http2"
)

// TransportBuilder is an F-bounded interface for configuring HTTP transport settings
// including connection pooling, timeouts, HTTP/2, and proxy configuration.
//
// Like all builders in this package, TransportBuilder is mutable: setter methods modify
// the receiver and return it for chaining. Use Clone to fork an independent copy.
//
// Generic functions can accept any TransportBuilder and return the same concrete type:
//
//	func ConfigureTransport[B TransportBuilder[B]](b B) B {
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

func (b *StandardClientBuilder) SetMaxIdleConns(n int) *StandardClientBuilder {
	b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
		p.MaxIdleConns = n
		return p
	})
	return b
}

func (b *StandardClientBuilder) SetMaxIdleConnsPerHost(n int) *StandardClientBuilder {
	b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
		p.MaxIdleConnsPerHost = n
		return p
	})
	return b
}

func (b *StandardClientBuilder) DisableKeepAlives() *StandardClientBuilder {
	b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
		p.DisableKeepAlives = true
		return p
	})
	return b
}

func (b *StandardClientBuilder) SetIdleConnTimeout(d time.Duration) *StandardClientBuilder {
	b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
		p.IdleConnTimeout = d
		return p
	})
	return b
}

func (b *StandardClientBuilder) SetExpectContinueTimeout(d time.Duration) *StandardClientBuilder {
	b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
		p.ExpectContinueTimeout = d
		return p
	})
	return b
}

func (b *StandardClientBuilder) SetResponseHeaderTimeout(d time.Duration) *StandardClientBuilder {
	b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
		p.ResponseHeaderTimeout = d
		return p
	})
	return b
}

func (b *StandardClientBuilder) SetTLSHandshakeTimeout(d time.Duration) *StandardClientBuilder {
	b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
		p.TLSHandshakeTimeout = d
		return p
	})
	return b
}

func (b *StandardClientBuilder) DisableHTTP2() *StandardClientBuilder {
	b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
		p.DisableHTTP2 = true
		return p
	})
	return b
}

func (b *StandardClientBuilder) SetHTTP2ReadIdleTimeout(d time.Duration) *StandardClientBuilder {
	b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
		p.HTTP2ReadIdleTimeout = d
		return p
	})
	return b
}

func (b *StandardClientBuilder) SetHTTP2PingTimeout(d time.Duration) *StandardClientBuilder {
	b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
		p.HTTP2PingTimeout = d
		return p
	})
	return b
}

func (b *StandardClientBuilder) SetHTTPProxyURL(s string) *StandardClientBuilder {
	if s == "" {
		b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
			p.HTTPProxyURL = nil
			return p
		})
		return b
	}
	proxyURL, err := url.Parse(s)
	if err != nil {
		b.errs = append(b.errs, werror.Wrap(err, "failed to parse HTTP proxy URL"))
		return b
	}
	b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
		p.HTTPProxyURL = proxyURL
		return p
	})
	return b
}

func (b *StandardClientBuilder) SetNoProxy() *StandardClientBuilder {
	b.dialerParams = refreshable.View(b.dialerParams, func(p dialerParams) dialerParams {
		p.SocksProxyURL = nil
		return p
	})
	b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
		p.HTTPProxyURL = nil
		p.ProxyFromEnvironment = false
		return p
	})
	return b
}

func (b *StandardClientBuilder) SetProxyFromEnvironment() *StandardClientBuilder {
	b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
		p.ProxyFromEnvironment = true
		return p
	})
	return b
}

// transportParams holds the parameters needed to build an HTTP transport.
type transportParams struct {
	MaxIdleConns          int
	MaxIdleConnsPerHost   int
	DisableHTTP2          bool
	DisableKeepAlives     bool
	IdleConnTimeout       time.Duration
	ExpectContinueTimeout time.Duration
	ResponseHeaderTimeout time.Duration
	TLSHandshakeTimeout   time.Duration
	HTTPProxyURL          *url.URL
	ProxyFromEnvironment  bool
	HTTP2ReadIdleTimeout  time.Duration
	HTTP2PingTimeout      time.Duration
}

// refreshableTransport implements http.RoundTripper backed by a refreshable *http.Transport.
// The transport and internal dialer are each rebuilt when any of their respective parameters are updated.
type refreshableTransport struct {
	Refreshable refreshable.Validated[*http.Transport]
}

func (r *refreshableTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return r.Refreshable.Unvalidated().RoundTrip(req)
}

func newTransport(ctx context.Context, p transportParams, tlsConfig *tls.Config, dialer ContextDialer) *http.Transport {
	var transportProxy func(*http.Request) (*url.URL, error)
	if p.HTTPProxyURL != nil {
		transportProxy = func(*http.Request) (*url.URL, error) { return p.HTTPProxyURL, nil }
	} else if p.ProxyFromEnvironment {
		transportProxy = http.ProxyFromEnvironment
	}

	transport := &http.Transport{
		Proxy:                 transportProxy,
		DialContext:           dialer.DialContext,
		MaxIdleConns:          p.MaxIdleConns,
		MaxIdleConnsPerHost:   p.MaxIdleConnsPerHost,
		TLSClientConfig:       tlsConfig,
		DisableKeepAlives:     p.DisableKeepAlives,
		ExpectContinueTimeout: p.ExpectContinueTimeout,
		IdleConnTimeout:       p.IdleConnTimeout,
		TLSHandshakeTimeout:   p.TLSHandshakeTimeout,
		ResponseHeaderTimeout: p.ResponseHeaderTimeout,
	}

	if !p.DisableHTTP2 {
		// Attempt to configure net/http HTTP/1 Transport to use HTTP/2.
		http2Transport, err := http2.ConfigureTransports(transport)
		if err != nil {
			// ConfigureTransport's only error as of this writing is the idempotent "protocol https already registered."
			// It should never happen in our usage because this is immediately after creation.
			// In case of something unexpected, log it and move on.
			svc1log.FromContext(ctx).Error("failed to configure transport for http2", svc1log.Stacktrace(err))
		} else {
			// ReadIdleTimeout is the amount of time to wait before running periodic health checks (pings)
			// after not receiving a frame from the HTTP/2 connection.
			// Setting this value will enable the health checks and allows broken idle
			// connections to be pruned more quickly, preventing the client from
			// attempting to re-use connections that will no longer work.
			// ref: https://github.com/golang/go/issues/36026
			http2Transport.ReadIdleTimeout = p.HTTP2ReadIdleTimeout

			// PingTimeout configures the amount of time to wait for a ping response (health check)
			// before closing an HTTP/2 connection. The PingTimeout is only valid if
			// the above ReadIdleTimeout is > 0.
			http2Transport.PingTimeout = p.HTTP2PingTimeout
		}
	}

	return transport
}

// BuildTransport builds the configured HTTP transport from the builder's transport,
// TLS, and dialer parameters. The returned http.RoundTripper automatically rebuilds
// when any underlying refreshable configuration changes.
//
// If SetTransport was called, the injected transport is returned directly.
// BuildTransport does NOT wrap the transport with middleware — use BuildHTTPClient
// or Build for the full middleware stack.
func (b *StandardClientBuilder) BuildTransport(ctx context.Context) (http.RoundTripper, error) {
	if len(b.errs) > 0 {
		return nil, werror.Error("builder configuration errors", werror.UnsafeParam("errors", b.errs))
	}
	if b.transport != nil {
		return b.transport, nil
	}
	tlsConfig, err := b.BuildTLSConfig(ctx)
	if err != nil {
		return nil, err
	}
	dialer, err := b.BuildDialer(ctx)
	if err != nil {
		return nil, err
	}
	rebuild := false
	mapped, _ := refreshable.MergeValidatedAndRefreshable(ctx, tlsConfig, b.transportParams, func(t *tls.Config, p transportParams) *http.Transport {
		if rebuild {
			svc1log.FromContext(ctx).Debug("Reconstructing HTTP Transport")
		} else {
			rebuild = true
		}
		return newTransport(ctx, p, t, dialer)
	})
	return &refreshableTransport{Refreshable: mapped}, nil
}
