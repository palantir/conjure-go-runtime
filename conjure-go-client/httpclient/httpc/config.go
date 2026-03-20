package httpc

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"os"
	"slices"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient"
	"github.com/palantir/pkg/refreshable/v2"
	werror "github.com/palantir/witchcraft-go-error"
)

type ClientConfig = httpclient.ClientConfig
type SecurityConfig = httpclient.SecurityConfig
type MetricsConfig = httpclient.MetricsConfig

// validatedClientParams holds validated, converted fields from a ClientConfig.
// Pointer/nil fields indicate "not set by config"; only non-nil fields should
// be applied to the builder, allowing ApplyConfig to compose with other builder
// calls without stomping unrelated settings.
type validatedClientParams struct {
	serviceName string   // set if non-empty
	uris        []string // non-nil = set (even if empty after filtering)

	// Auth — at most one of apiToken or basicAuth is set.
	apiToken  *string
	basicAuth *BasicAuth

	// Timeout — set only when ReadTimeout or WriteTimeout is in config.
	timeout *time.Duration

	// Dialer fields — nil means "not specified in config".
	connectTimeout *time.Duration
	keepAlive      *time.Duration
	socksProxyURL  *url.URL

	// Transport fields — nil means "not specified in config".
	maxIdleConns          *int
	maxIdleConnsPerHost   *int
	disableHTTP2          *bool
	idleConnTimeout       *time.Duration
	expectContinueTimeout *time.Duration
	responseHeaderTimeout *time.Duration
	tlsHandshakeTimeout   *time.Duration
	proxyFromEnvironment  *bool
	httpProxyURL          *url.URL
	http2ReadIdleTimeout  *time.Duration
	http2PingTimeout      *time.Duration

	// TLS — individual fields; zero values mean "not specified".
	caFiles            []string
	certFile           string
	keyFile            string
	insecureSkipVerify *bool

	// Retry
	maxAttempts    *int
	initialBackoff *time.Duration
	maxBackoff     *time.Duration

	// Metrics
	disableMetrics *bool
	metricsTags    Tags
}

// newValidatedClientParams validates a ClientConfig and converts it to
// httpc-native types. Only fields explicitly set in the config are populated;
// unset fields remain at their zero/nil value.
func newValidatedClientParams(ctx context.Context, config ClientConfig) (validatedClientParams, error) {
	var p validatedClientParams

	p.serviceName = config.ServiceName

	// URIs — validate and filter.
	if len(config.URIs) > 0 {
		uris := make([]string, 0, len(config.URIs))
		for _, uriStr := range config.URIs {
			if uriStr == "" {
				continue
			}
			if _, err := url.ParseRequestURI(uriStr); err != nil {
				return validatedClientParams{}, werror.WrapWithContextParams(ctx, err, "invalid url")
			}
			uris = append(uris, uriStr)
		}
		slices.Sort(uris)
		p.uris = uris
	}

	// Auth — APIToken > APITokenFile > BasicAuth.
	if config.APIToken != nil {
		p.apiToken = config.APIToken
	} else if config.APITokenFile != nil {
		file := *config.APITokenFile
		token, err := os.ReadFile(file)
		if err != nil {
			return validatedClientParams{}, werror.WrapWithContextParams(ctx, err, "failed to read api-token-file", werror.SafeParam("file", file))
		}
		p.apiToken = new(string(bytes.TrimSpace(token)))
	} else if config.BasicAuth != nil && config.BasicAuth.User != "" && config.BasicAuth.Password != "" {
		p.basicAuth = &BasicAuth{
			User:     config.BasicAuth.User,
			Password: config.BasicAuth.Password,
		}
	}

	// Timeout — only set when the config explicitly specifies read/write timeout.
	if config.ReadTimeout != nil || config.WriteTimeout != nil {
		p.timeout = new(max(derefPtr(config.ReadTimeout, 0), derefPtr(config.WriteTimeout, 0)))
	}

	// Proxy — validate and classify.
	if config.ProxyURL != nil {
		proxyURL, err := url.ParseRequestURI(*config.ProxyURL)
		if err != nil {
			return validatedClientParams{}, werror.WrapWithContextParams(ctx, err, "invalid proxy url")
		}
		switch proxyURL.Scheme {
		case "http", "https":
			p.httpProxyURL = proxyURL
		case "socks5", "socks5h":
			p.socksProxyURL = proxyURL
		default:
			return validatedClientParams{}, werror.ErrorWithContextParams(ctx, "invalid proxy url: only http(s) and socks5 are supported")
		}
	}

	// Dialer fields — pass through only if set.
	p.connectTimeout = config.ConnectTimeout
	p.keepAlive = config.KeepAlive

	// Transport fields — pass through only if set.
	p.maxIdleConns = config.MaxIdleConns
	p.maxIdleConnsPerHost = config.MaxIdleConnsPerHost
	p.disableHTTP2 = config.DisableHTTP2
	p.idleConnTimeout = config.IdleConnTimeout
	p.expectContinueTimeout = config.ExpectContinueTimeout
	p.responseHeaderTimeout = config.ResponseHeaderTimeout
	p.tlsHandshakeTimeout = config.TLSHandshakeTimeout
	p.proxyFromEnvironment = config.ProxyFromEnvironment
	p.http2ReadIdleTimeout = config.HTTP2ReadIdleTimeout
	p.http2PingTimeout = config.HTTP2PingTimeout

	// TLS
	p.caFiles = config.Security.CAFiles
	p.certFile = config.Security.CertFile
	p.keyFile = config.Security.KeyFile
	p.insecureSkipVerify = config.Security.InsecureSkipVerify

	// Retry — convert MaxNumRetries to maxAttempts.
	if config.MaxNumRetries != nil {
		p.maxAttempts = new(*config.MaxNumRetries + 1)
	}
	p.initialBackoff = config.InitialBackoff
	p.maxBackoff = config.MaxBackoff

	// Metrics
	if config.Metrics.Enabled != nil {
		disabled := !*config.Metrics.Enabled
		p.disableMetrics = &disabled
	}
	if len(config.Metrics.Tags) > 0 {
		p.metricsTags = config.Metrics.Tags
	}

	return p, nil
}

// applyValidatedParams calls builder setter methods only for fields that the
// config explicitly specified. Unset fields are left at whatever the builder
// already has, preserving composability with other builder calls.
func applyValidatedParams(b *StandardClientBuilder, p validatedClientParams) {
	if p.serviceName != "" {
		b.SetServiceName(p.serviceName)
	}
	if p.uris != nil {
		b.SetBaseURLs(p.uris...)
	}

	// Auth — only install the middleware the config specifies.
	if p.apiToken != nil {
		b.SetAuthToken(*p.apiToken)
	} else if p.basicAuth != nil {
		b.SetBasicAuth(p.basicAuth.User, p.basicAuth.Password)
	}

	if p.timeout != nil {
		b.SetTimeout(*p.timeout)
	}

	// Dialer fields.
	if p.connectTimeout != nil {
		b.SetDialTimeout(*p.connectTimeout)
	}
	if p.keepAlive != nil {
		b.SetKeepAlive(*p.keepAlive)
	}
	if p.socksProxyURL != nil {
		b.SetSocksProxyURL(p.socksProxyURL.String())
	}

	// Transport fields.
	if p.httpProxyURL != nil {
		b.SetHTTPProxyURL(p.httpProxyURL.String())
	}
	if p.proxyFromEnvironment != nil {
		if *p.proxyFromEnvironment {
			b.SetProxyFromEnvironment()
		} else {
			b.transportParams = refreshable.View(b.transportParams, func(tp transportParams) transportParams {
				tp.ProxyFromEnvironment = false
				return tp
			})
		}
	}
	if p.maxIdleConns != nil {
		b.SetMaxIdleConns(*p.maxIdleConns)
	}
	if p.maxIdleConnsPerHost != nil {
		b.SetMaxIdleConnsPerHost(*p.maxIdleConnsPerHost)
	}
	if p.disableHTTP2 != nil && *p.disableHTTP2 {
		b.DisableHTTP2()
	}
	if p.idleConnTimeout != nil {
		b.SetIdleConnTimeout(*p.idleConnTimeout)
	}
	if p.expectContinueTimeout != nil {
		b.SetExpectContinueTimeout(*p.expectContinueTimeout)
	}
	if p.responseHeaderTimeout != nil {
		b.SetResponseHeaderTimeout(*p.responseHeaderTimeout)
	}
	if p.tlsHandshakeTimeout != nil {
		b.SetTLSHandshakeTimeout(*p.tlsHandshakeTimeout)
	}
	if p.http2ReadIdleTimeout != nil {
		b.SetHTTP2ReadIdleTimeout(*p.http2ReadIdleTimeout)
	}
	if p.http2PingTimeout != nil {
		b.SetHTTP2PingTimeout(*p.http2PingTimeout)
	}

	// TLS
	if len(p.caFiles) > 0 {
		b.AddCACertFiles(p.caFiles...)
	}
	if p.certFile != "" && p.keyFile != "" {
		b.SetClientCertFiles(p.keyFile, p.certFile)
	}
	if p.insecureSkipVerify != nil {
		b.SetInsecureSkipVerify(*p.insecureSkipVerify)
	}

	// Retry
	if p.maxAttempts != nil {
		b.SetMaxAttempts(p.maxAttempts)
	}
	if p.initialBackoff != nil {
		b.SetInitialBackoff(*p.initialBackoff)
	}
	if p.maxBackoff != nil {
		b.SetMaxBackoff(*p.maxBackoff)
	}

	// Metrics
	if p.disableMetrics != nil {
		b.SetDisableMetrics(*p.disableMetrics)
	}
	if len(p.metricsTags) > 0 {
		b.metricsTagProviders = append(b.metricsTagProviders, p.metricsTags)
	}
}

// ApplyConfig validates the config and calls builder setter methods for each
// field the config explicitly specifies. Fields not present in the config are
// left at whatever the builder already has (from NewStandardClientBuilder
// defaults or prior setter calls), preserving composability.
// Validation errors are deferred to b.errs (surfaced at Build time).
func (b *StandardClientBuilder) ApplyConfig(ctx context.Context, config ClientConfig) *StandardClientBuilder {
	params, err := newValidatedClientParams(ctx, config)
	if err != nil {
		b.errs = append(b.errs, err)
		return b
	}
	applyValidatedParams(b, params)
	return b
}

// ApplyConfigRefreshable validates the initial config and wires up refreshable
// overlays for each field the config specifies. When a config field is unset,
// the builder's existing value (captured at call time) is used as the fallback.
// Validation errors are deferred to b.errs (surfaced at Build time).
func (b *StandardClientBuilder) ApplyConfigRefreshable(ctx context.Context, config refreshable.Refreshable[ClientConfig]) *StandardClientBuilder {
	// Stage 1: Validate initial config and create validated refreshable.
	validParams, _, err := refreshable.MapWithError(ctx, config, newValidatedClientParams)
	if err != nil {
		b.errs = append(b.errs, err)
		return b
	}

	// Stage 2: For each field, create a refreshable that overlays the config
	// value (when set) on top of the builder's existing value (captured now).

	existingServiceName := b.serviceName
	b.serviceName, _ = refreshable.MapFromValidated(validParams, func(p validatedClientParams) string {
		if p.serviceName != "" {
			return p.serviceName
		}
		return existingServiceName.Current()
	})

	existingURIs := b.uris
	uris, _ := refreshable.MapFromValidated(validParams, func(p validatedClientParams) []string {
		if p.uris != nil {
			return p.uris
		}
		if existingURIs != nil {
			return existingURIs.Current()
		}
		return nil
	})
	b.uris = uris

	existingTimeout := b.timeout
	b.timeout, _ = refreshable.MapFromValidated(validParams, func(p validatedClientParams) time.Duration {
		if p.timeout != nil {
			return *p.timeout
		}
		return existingTimeout.Current()
	})

	// Dialer: overlay individual config fields onto existing dialer params.
	existingDialer := b.dialerParams
	b.dialerParams, _ = refreshable.MapFromValidated(validParams, func(p validatedClientParams) dialerParams {
		d := existingDialer.Current()
		if p.connectTimeout != nil {
			d.DialTimeout = *p.connectTimeout
		}
		if p.keepAlive != nil {
			d.KeepAlive = *p.keepAlive
		}
		if p.socksProxyURL != nil {
			d.SocksProxyURL = p.socksProxyURL
		}
		return d
	})

	// Transport: overlay individual config fields onto existing transport params.
	existingTransport := b.transportParams
	b.transportParams, _ = refreshable.MapFromValidated(validParams, func(p validatedClientParams) transportParams {
		t := existingTransport.Current()
		if p.maxIdleConns != nil {
			t.MaxIdleConns = *p.maxIdleConns
		}
		if p.maxIdleConnsPerHost != nil {
			t.MaxIdleConnsPerHost = *p.maxIdleConnsPerHost
		}
		if p.disableHTTP2 != nil {
			t.DisableHTTP2 = *p.disableHTTP2
		}
		if p.idleConnTimeout != nil {
			t.IdleConnTimeout = *p.idleConnTimeout
		}
		if p.expectContinueTimeout != nil {
			t.ExpectContinueTimeout = *p.expectContinueTimeout
		}
		if p.responseHeaderTimeout != nil {
			t.ResponseHeaderTimeout = *p.responseHeaderTimeout
		}
		if p.tlsHandshakeTimeout != nil {
			t.TLSHandshakeTimeout = *p.tlsHandshakeTimeout
		}
		if p.proxyFromEnvironment != nil {
			t.ProxyFromEnvironment = *p.proxyFromEnvironment
		}
		if p.httpProxyURL != nil {
			t.HTTPProxyURL = p.httpProxyURL
		}
		if p.http2ReadIdleTimeout != nil {
			t.HTTP2ReadIdleTimeout = *p.http2ReadIdleTimeout
		}
		if p.http2PingTimeout != nil {
			t.HTTP2PingTimeout = *p.http2PingTimeout
		}
		return t
	})

	// TLS: overlay individual config fields onto existing TLS file params.
	existingTLS := b.tlsFileParams
	b.tlsFileParams, _ = refreshable.MapFromValidated(validParams, func(p validatedClientParams) tlsFileParams {
		t := existingTLS.Current()
		if len(p.caFiles) > 0 {
			t.CAFiles = p.caFiles
		}
		if p.certFile != "" {
			t.CertFile = p.certFile
		}
		if p.keyFile != "" {
			t.KeyFile = p.keyFile
		}
		if p.insecureSkipVerify != nil {
			t.InsecureSkipVerify = *p.insecureSkipVerify
		}
		return t
	})

	existingMaxAttempts := b.maxAttempts
	b.maxAttempts, _ = refreshable.MapFromValidated(validParams, func(p validatedClientParams) *int {
		if p.maxAttempts != nil {
			return p.maxAttempts
		}
		if existingMaxAttempts != nil {
			return existingMaxAttempts.Current()
		}
		return nil
	})

	existingInitialBackoff := b.initialBackoff
	b.initialBackoff, _ = refreshable.MapFromValidated(validParams, func(p validatedClientParams) time.Duration {
		if p.initialBackoff != nil {
			return *p.initialBackoff
		}
		return existingInitialBackoff.Current()
	})

	existingMaxBackoff := b.maxBackoff
	b.maxBackoff, _ = refreshable.MapFromValidated(validParams, func(p validatedClientParams) time.Duration {
		if p.maxBackoff != nil {
			return *p.maxBackoff
		}
		return existingMaxBackoff.Current()
	})

	existingDisableMetrics := b.disableMetrics
	b.disableMetrics, _ = refreshable.MapFromValidated(validParams, func(p validatedClientParams) bool {
		if p.disableMetrics != nil {
			return *p.disableMetrics
		}
		return existingDisableMetrics.Current()
	})

	// Auth: always install both middlewares for the refreshable case, since
	// the config may switch between token and basic auth on refresh.
	// Each middleware checks for nil and becomes a no-op when unset.
	apiToken, _ := refreshable.MapFromValidated(validParams, func(p validatedClientParams) *string { return p.apiToken })
	basicAuth, _ := refreshable.MapFromValidated(validParams, func(p validatedClientParams) *BasicAuth { return p.basicAuth })
	b.SetAuthTokenRefreshable(apiToken)
	b.SetBasicAuthRefreshable(basicAuth)

	// Metrics tags via closure over validated refreshable.
	b.metricsTagProviders = append(b.metricsTagProviders, TagsProviderFunc(func(*http.Request, *http.Response, error) Tags {
		return validParams.Unvalidated().metricsTags
	}))

	return b
}

func derefPtr[T any](ptr *T, defaultVal T) T {
	if ptr == nil {
		return defaultVal
	}
	return *ptr
}
