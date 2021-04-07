package refreshabletransport

import (
	"context"
	"net"
	"net/url"
	"time"

	"github.com/palantir/pkg/refreshable"
	"github.com/palantir/witchcraft-go-logging/wlog/svclog/svc1log"
	"golang.org/x/net/proxy"
)

type DialerParams struct {
	DialTimeout   time.Duration
	KeepAlive     time.Duration
	SocksProxyURL *url.URL
}

// ContextDialer is the interface implemented by net.Dialer, proxy.Dialer, and others
type ContextDialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

func NewRefreshableDialer(ctx context.Context, p RefreshableDialerParams) ContextDialer {
	return &RefreshableDialer{
		Refreshable: p.Map(func(i interface{}) interface{} {
			p := i.(DialerParams)

			var dialer ContextDialer = &net.Dialer{
				Timeout:   p.DialTimeout,
				KeepAlive: p.KeepAlive,
			}
			if p.SocksProxyURL == nil {
				return dialer
			}
			proxyDialer, err := proxy.FromURL(p.SocksProxyURL, dialer.(proxy.Dialer))
			if err != nil {
				// should never happen; checked in the validating refreshable
				svc1log.FromContext(ctx).Error("Failed to construct socks5 dialer", svc1log.Stacktrace(err))
				return dialer
			}
			return proxyDialer.(ContextDialer)
		}),
	}
}

type RefreshableDialerParams struct {
	refreshable.Refreshable // contains DialerParams
}

func (r RefreshableDialerParams) CurrentDialerParams() DialerParams {
	return r.Current().(DialerParams)
}

// TransformParams accepts a mapping function which will be applied to the params value as it is evaluated.
// This can be used to layer/overwrite configuration before building the RefreshableDialer.
func (r RefreshableDialerParams) TransformParams(mapFn func(p DialerParams) DialerParams) RefreshableDialerParams {
	return RefreshableDialerParams{
		Refreshable: r.Map(func(i interface{}) interface{} {
			return mapFn(i.(DialerParams))
		}),
	}
}

type RefreshableDialer struct {
	refreshable.Refreshable // contains ContextDialer
}

func (r *RefreshableDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return r.Current().(ContextDialer).DialContext(ctx, network, address)
}
