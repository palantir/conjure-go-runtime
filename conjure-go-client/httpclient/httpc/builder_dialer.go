package httpc

import (
	"context"
	"net"
	"net/url"
	"time"

	"github.com/palantir/pkg/refreshable/v2"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/palantir/witchcraft-go-logging/wlog/svclog/svc1log"
	"golang.org/x/net/proxy"
)

// DialerBuilder is an F-bounded interface for configuring TCP dialer settings.
//
// Like all builders in this package, DialerBuilder is mutable: setter methods modify
// the receiver and return it for chaining. Use Clone to fork an independent copy.
//
// Generic functions can accept any DialerBuilder and return the same concrete type:
//
//	func ConfigureDialer[B DialerBuilder[B]](b B) B {
//	    return b.SetDialTimeout(5 * time.Second).SetKeepAlive(15 * time.Second)
//	}
type DialerBuilder[B DialerBuilder[B]] interface {
	// Clone returns a deep copy of the builder. The copy is fully independent:
	// mutations to either the original or the clone do not affect the other.
	Clone() B

	// Apply applies the given Param functions to the builder in sequence.
	// Each Param may call setter methods to configure the builder.
	// Because builders are mutable, this modifies the receiver in place.
	Apply(...Param[B]) B

	// SetDialTimeout sets the maximum duration for establishing a TCP connection.
	// Default: 10s.
	SetDialTimeout(time.Duration) B

	// SetKeepAlive sets the interval between TCP keep-alive probes.
	// Default: 30s.
	SetKeepAlive(time.Duration) B

	// SetSocksProxyURL sets a SOCKS5 proxy URL for TCP connections.
	// Pass "" to clear. Only socks5:// URLs are supported.
	// HTTP/HTTPS proxy configuration is on TransportBuilder.
	SetSocksProxyURL(string) B
}

func (b *StandardClientBuilder) SetDialTimeout(d time.Duration) *StandardClientBuilder {
	b.dialerParams = refreshable.View(b.dialerParams, func(p dialerParams) dialerParams {
		p.DialTimeout = d
		return p
	})
	return b
}

func (b *StandardClientBuilder) SetKeepAlive(d time.Duration) *StandardClientBuilder {
	b.dialerParams = refreshable.View(b.dialerParams, func(p dialerParams) dialerParams {
		p.KeepAlive = d
		return p
	})
	return b
}

func (b *StandardClientBuilder) SetSocksProxyURL(s string) *StandardClientBuilder {
	if s == "" {
		b.dialerParams = refreshable.View(b.dialerParams, func(p dialerParams) dialerParams {
			p.SocksProxyURL = nil
			return p
		})
		return b
	}
	proxyURL, err := url.Parse(s)
	if err != nil {
		b.errs = append(b.errs, werror.Wrap(err, "failed to parse SOCKS proxy URL"))
		return b
	}
	b.dialerParams = refreshable.View(b.dialerParams, func(p dialerParams) dialerParams {
		p.SocksProxyURL = proxyURL
		return p
	})
	return b
}

// ContextDialer is the interface implemented by net.Dialer, proxy.Dialer, and others.
type ContextDialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
	Dial(network, address string) (net.Conn, error)
}

// dialerParams holds the parameters needed to build a TCP dialer.
type dialerParams struct {
	DialTimeout   time.Duration
	KeepAlive     time.Duration
	SocksProxyURL *url.URL
}

type refreshableDialer struct {
	refreshable.Refreshable[ContextDialer]
}

func (r *refreshableDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return r.Current().DialContext(ctx, network, address)
}

func (r *refreshableDialer) Dial(network, address string) (net.Conn, error) {
	return r.Current().DialContext(context.TODO(), network, address)
}

// BuildDialer builds the configured TCP dialer from the builder's dialer parameters.
// The returned ContextDialer automatically adapts when dial timeout, keep-alive,
// or SOCKS proxy settings change via their refreshable sources.
func (b *StandardClientBuilder) BuildDialer(ctx context.Context) (ContextDialer, error) {
	if len(b.errs) > 0 {
		return nil, werror.Error("builder configuration errors", werror.UnsafeParam("errors", b.errs))
	}
	mapped, _ := refreshable.Map(b.dialerParams, func(p dialerParams) ContextDialer {
		svc1log.FromContext(ctx).Debug("Reconstructing HTTP Dialer")
		dialer := &net.Dialer{
			Timeout:   p.DialTimeout,
			KeepAlive: p.KeepAlive,
		}
		if p.SocksProxyURL == nil {
			return dialer
		}
		proxyDialer, err := proxy.FromURL(p.SocksProxyURL, dialer)
		if err != nil {
			// should never happen; checked in the validating refreshable
			svc1log.FromContext(ctx).Error("Failed to construct socks5 dialer. Please report this as a bug in conjure-go-runtime.", svc1log.Stacktrace(err))
			return dialer
		}
		// proxy.Dialer interface only has Dial(), but we need DialContext.
		// The underlying dialer already implements DialContext, so we can safely cast if it's *net.Dialer.
		if contextDialer, ok := proxyDialer.(ContextDialer); ok {
			return contextDialer
		}
		return dialer
	})
	return &refreshableDialer{Refreshable: mapped}, nil
}
