package httpclient

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/palantir/pkg/metrics"
	"github.com/palantir/pkg/refreshable"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/palantir/witchcraft-go-logging/wlog/svclog/svc1log"
	"golang.org/x/net/http2"
	"golang.org/x/net/proxy"
)

const (
	defaultDialTimeout           = 5 * time.Second
	defaultHTTPTimeout           = 60 * time.Second
	defaultKeepAlive             = 30 * time.Second
	defaultIdleConnTimeout       = 90 * time.Second
	defaultTLSHandshakeTimeout   = 10 * time.Second
	defaultExpectContinueTimeout = 1 * time.Second
	defaultMaxIdleConns          = 200
	defaultMaxIdleConnsPerHost   = 100
)

type RefreshableHTTPClient interface {
	refreshable.Refreshable
	CurrentHTTPClient() *http.Client
}

type refreshableHTTPClient struct {
	refreshable.Refreshable
}

func (r refreshableHTTPClient) CurrentHTTPClient() *http.Client {
	return r.Current().(*http.Client)
}

type refreshableClientParams struct {
	DisableRecovery    refreshable.Bool
	DisableTracing     refreshable.Bool
	DisableMetrics     refreshable.Bool
	MetricTagProviders []TagsProvider
	ServiceNameTag     metrics.Tag
	Timeout            refreshable.DurationPtr // nil -> default of 60s. <= 0 -> no timeout.
	Transport          refreshableTransportParams
}

func newRefreshableHTTPClient(ctx context.Context, p refreshableClientParams) (*refreshableHTTPClient, error) {
	transport, err := newRefreshableTransport(ctx, p.Transport)
	if err != nil {
		return nil, err
	}
	var rt http.RoundTripper = transport
	rt = wrapTransport(rt, newMetricsMiddleware(p.ServiceNameTag, p.MetricTagProviders, p.DisableMetrics))
	rt = wrapTransport(rt, traceMiddleware{Disabled: p.DisableTracing, ServiceName: p.ServiceNameTag.Value()})
	rt = wrapTransport(rt, recoveryMiddleware{Disabled: p.DisableRecovery})

	timeout := refreshable.NewDuration(p.Timeout.MapDurationPtr(func(t *time.Duration) interface{} {
		switch {
		case t == nil:
			return defaultHTTPTimeout
		case *t <= 0:
			return 0
		default:
			return *t
		}
	}))
	client := timeout.MapDuration(func(t time.Duration) interface{} {
		return &http.Client{
			Transport: transport,
			Timeout:   t,
		}
	})
	return &refreshableHTTPClient{Refreshable: client}, nil
}

type refreshableTransportParams struct {
	ServiceNameTag                metrics.Tag
	MetricTagProviders            []TagsProvider
	DialTimeout                   refreshable.Duration
	DisableHTTP2                  refreshable.Bool
	DisableMetrics                refreshable.Bool
	DisableTraceHeaderPropagation refreshable.Bool
	DisableKeepAlives             refreshable.Bool
	IdleConnTimeout               refreshable.Duration
	EnableIPV6                    refreshable.Bool
	ExpectContinueTimeout         refreshable.Duration
	KeepAlive                     refreshable.Duration
	MaxIdleConns                  refreshable.Int
	MaxIdleConnsPerHost           refreshable.Int
	ProxyFromEnvironment          refreshable.Bool
	ProxyURL                      refreshable.String
	ResponseHeaderTimeout         refreshable.Duration
	TLSClientConfig               refreshable.Refreshable
	TLSHandshakeTimeout           refreshable.Duration
}

type snapshotTransportParams struct {
	ServiceNameTag                metrics.Tag
	MaxIdleConns                  int
	MaxIdleConnsPerHost           int
	DisableHTTP2                  bool
	DisableTraceHeaderPropagation bool
	DisableKeepAlives             bool
	IdleConnTimeout               time.Duration
	ExpectContinueTimeout         time.Duration
	ResponseHeaderTimeout         time.Duration
	MetricTagProviders            []TagsProvider
	ProxyFromEnvironment          bool
	ProxyURL                      *url.URL
	TLSClientConfig               *tls.Config
	TLSHandshakeTimeout           time.Duration
}

type snapshotDialerParams struct {
	ServiceNameTag metrics.Tag
	DialTimeout    time.Duration
	KeepAlive      time.Duration
	EnableIPV6     bool
	ProxyURL       *url.URL
}

type refreshableTransport struct {
	transport refreshable.Refreshable // contains *http.Transport
}

func (r *refreshableTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return r.transport.Current().(*http.Transport).RoundTrip(req)
}

// newRefreshableTransport returns an implementation of http.RoundTripper backed by a refreshable *http.Transport.
// The transport and internal dialer are each rebuilt when any of their respective parameters are updated.
func newRefreshableTransport(ctx context.Context, transportParams refreshableTransportParams) (*refreshableTransport, error) {
	proxyURLRefreshable, err := newRefreshableURL(transportParams.ProxyURL)
	if err != nil {
		return nil, err
	}

	dialerParamsRefreshable, _ := refreshable.MapAll([]refreshable.Refreshable{
		transportParams.DialTimeout,
		transportParams.KeepAlive,
		transportParams.EnableIPV6,
		proxyURLRefreshable,
	}, func(vals []interface{}) interface{} {
		p := snapshotDialerParams{
			ServiceNameTag: transportParams.ServiceNameTag,
			DialTimeout:    vals[0].(time.Duration),
			KeepAlive:      vals[1].(time.Duration),
			EnableIPV6:     vals[2].(bool),
			ProxyURL:       vals[3].(*url.URL),
		}
		if p.DialTimeout <= 0 {
			p.DialTimeout = defaultDialTimeout
		}
		if p.KeepAlive <= 0 {
			p.KeepAlive = defaultKeepAlive
		}
		return p
	})

	dialer := &metricsWrappedDialer{
		Disabled:       transportParams.DisableMetrics,
		ServiceNameTag: transportParams.ServiceNameTag,
		Dialer: &refreshableDialer{
			dialer: dialerParamsRefreshable.Map(func(i interface{}) interface{} {
				return newDialer(ctx, i.(snapshotDialerParams))
			}),
		},
	}

	transportParamsRefreshable, _ := refreshable.MapAll([]refreshable.Refreshable{
		transportParams.MaxIdleConns,
		transportParams.MaxIdleConnsPerHost,
		transportParams.DisableHTTP2,
		transportParams.DisableKeepAlives,
		transportParams.ExpectContinueTimeout,
		transportParams.IdleConnTimeout,
		transportParams.ResponseHeaderTimeout,
		transportParams.ProxyFromEnvironment,
		transportParams.TLSClientConfig,
		transportParams.TLSHandshakeTimeout,
		proxyURLRefreshable,
	}, func(vals []interface{}) interface{} {
		p := snapshotTransportParams{
			ServiceNameTag:        transportParams.ServiceNameTag,
			MaxIdleConns:          vals[0].(int),
			MaxIdleConnsPerHost:   vals[1].(int),
			DisableHTTP2:          vals[2].(bool),
			DisableKeepAlives:     vals[3].(bool),
			ExpectContinueTimeout: vals[4].(time.Duration),
			IdleConnTimeout:       vals[5].(time.Duration),
			ResponseHeaderTimeout: vals[6].(time.Duration),
			ProxyFromEnvironment:  vals[7].(bool),
			TLSClientConfig:       vals[8].(*tls.Config),
			TLSHandshakeTimeout:   vals[9].(time.Duration),
			ProxyURL:              vals[10].(*url.URL),
		}
		return p
	})
	transport, err := refreshable.MapValidatingRefreshable(transportParamsRefreshable, func(i interface{}) (interface{}, error) {
		return newTransport(ctx, i.(snapshotTransportParams), dialer)
	})
	if err != nil {
		return nil, err
	}
	return &refreshableTransport{transport: transport}, nil
}

func newTransport(ctx context.Context, p snapshotTransportParams, dialer contextDialer) (*http.Transport, error) {
	if p.MaxIdleConns <= 0 {
		p.MaxIdleConns = defaultMaxIdleConns
	}
	if p.MaxIdleConnsPerHost <= 0 {
		p.MaxIdleConnsPerHost = defaultMaxIdleConnsPerHost
	}
	if p.IdleConnTimeout <= 0 {
		p.IdleConnTimeout = defaultIdleConnTimeout
	}
	if p.TLSHandshakeTimeout <= 0 {
		p.TLSHandshakeTimeout = defaultTLSHandshakeTimeout
	}
	if p.ExpectContinueTimeout <= 0 {
		p.ExpectContinueTimeout = defaultExpectContinueTimeout
	}

	svc1log.FromContext(ctx).Debug("New http transport with params", svc1log.SafeParam("transportParams", p))
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		MaxIdleConns:          p.MaxIdleConns,
		MaxIdleConnsPerHost:   p.MaxIdleConnsPerHost,
		TLSClientConfig:       p.TLSClientConfig,
		DisableKeepAlives:     p.DisableKeepAlives,
		ExpectContinueTimeout: p.ExpectContinueTimeout,
		IdleConnTimeout:       p.IdleConnTimeout,
		TLSHandshakeTimeout:   p.TLSHandshakeTimeout,
		ResponseHeaderTimeout: p.ResponseHeaderTimeout,
	}
	if p.ProxyURL != nil && (p.ProxyURL.Scheme == "http" || p.ProxyURL.Scheme == "https") {
		transport.Proxy = func(*http.Request) (*url.URL, error) { return p.ProxyURL, nil }
	} else if p.ProxyFromEnvironment {
		transport.Proxy = http.ProxyFromEnvironment
	}

	if !p.DisableHTTP2 {
		if err := http2.ConfigureTransport(transport); err != nil {
			return nil, werror.Wrap(err, "failed to configure transport for http2")
		}
	}
	return transport, nil
}

type contextDialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

type refreshableDialer struct {
	dialer refreshable.Refreshable // contains contextDialer
}

func (r *refreshableDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return r.dialer.Current().(contextDialer).DialContext(ctx, network, address)
}

func newDialer(ctx context.Context, p snapshotDialerParams) contextDialer {
	if p.DialTimeout <= 0 {
		p.DialTimeout = defaultDialTimeout
	}
	if p.KeepAlive <= 0 {
		p.KeepAlive = defaultKeepAlive
	}

	svc1log.FromContext(ctx).Debug("New http dialer with params", svc1log.SafeParam("dialerParams", p))
	var dialer contextDialer = &net.Dialer{
		Timeout:   p.DialTimeout,
		KeepAlive: p.KeepAlive,
		DualStack: p.EnableIPV6,
	}
	if p.ProxyURL != nil && (p.ProxyURL.Scheme == "socks5" || p.ProxyURL.Scheme == "socks5h") {
		if proxyDialer, err := proxy.FromURL(p.ProxyURL, dialer.(proxy.Dialer)); err != nil {
			// should never happen; checked in the validating refreshable
			svc1log.FromContext(ctx).Error("Failed to construct socks5 dialer", svc1log.Stacktrace(err))
		} else {
			dialer = proxyDialer.(contextDialer)
		}
	}
	return dialer
}

func newRefreshableURL(refreshableString refreshable.String) (refreshable.Refreshable, error) {
	return refreshable.MapValidatingRefreshable(refreshableString,
		func(i interface{}) (interface{}, error) {
			if s := i.(string); s != "" {
				u, err := url.Parse(s)
				if err != nil {
					return nil, werror.Wrap(err, "invalid proxy url")
				}
				switch u.Scheme {
				case "http", "https", "socks5", "socks5h":
				default:
					return nil, werror.Wrap(err, "invalid proxy url scheme")
				}
			}
			return nil, nil
		},
	)
}
