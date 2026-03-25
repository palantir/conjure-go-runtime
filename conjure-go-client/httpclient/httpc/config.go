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

package httpc

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"os"
	"slices"
	"time"

	"github.com/palantir/pkg/metrics"
	"github.com/palantir/pkg/refreshable/v2"
	werror "github.com/palantir/witchcraft-go-error"
)

// ServicesConfig is the top-level configuration struct for all HTTP clients. It supports
// setting default values and overriding those values per-service. Use ClientConfig(serviceName)
// to retrieve a specific service's configuration, and the httpclient.WithConfig() param to
// construct a Client using that configuration. The fields of this struct should generally not
// be read directly by application code.
type ServicesConfig struct {
	// Default values will be used for any field which is not set for a specific client.
	Default ClientConfig `json:",inline" yaml:",inline"`
	// Services is a map of serviceName (e.g. "my-api") to service-specific configuration.
	Services map[string]ClientConfig `json:"services,omitempty" yaml:"services,omitempty"`
}

// ClientConfig represents the configuration for a single REST client.
type ClientConfig struct {
	ServiceName string `json:"-" yaml:"-"`
	// URIs is a list of fully specified base URIs for the service. These can optionally include a path
	// which will be prepended to the request path specified when invoking the client.
	URIs []string `json:"uris,omitempty" yaml:"uris,omitempty"`
	// APIToken is a string which, if provided, will be used as a Bearer token in the Authorization header.
	// This takes precedence over APITokenFile.
	APIToken *string `json:"api-token,omitempty" yaml:"api-token,omitempty"`
	// APITokenFile is an on-disk location containing a Bearer token. If APITokenFile is provided and APIToken
	// is not, the content of the file will be used as the APIToken.
	APITokenFile *string `json:"api-token-file,omitempty" yaml:"api-token-file,omitempty"`
	// BasicAuth is a user/password combination which, if provided, will be used as the credentials in the
	// Authorization header. APIToken and APITokenFile will take precedent over BasicAuth if specified
	BasicAuth *BasicAuth `json:"basic-auth,omitempty" yaml:"basic-auth,omitempty"`
	// DisableHTTP2, if true, will prevent the client from modifying the *tls.Config object to support H2 connections.
	DisableHTTP2 *bool `json:"disable-http2,omitempty" yaml:"disable-http2,omitempty"`
	// ProxyFromEnvironment enables reading HTTP proxy information from environment variables.
	// See 'http.ProxyFromEnvironment' documentation for specific behavior.
	ProxyFromEnvironment *bool `json:"proxy-from-environment,omitempty" yaml:"proxy-from-environment,omitempty"`
	// ProxyURL uses the provided URL for proxying the request. Schemes http, https, and socks5 are supported.
	ProxyURL *string `json:"proxy-url,omitempty" yaml:"proxy-url,omitempty"`

	// MaxNumRetries controls the number of times the client will retry retryable failures.
	// If unset, this defaults to twice the number of URIs provided.
	MaxNumRetries *int `json:"max-num-retries,omitempty" yaml:"max-num-retries,omitempty"`
	// InitialBackoff controls the duration of the first backoff interval. This delay will double for each subsequent backoff, capped at the MaxBackoff value.
	InitialBackoff *time.Duration `json:"initial-backoff,omitempty" yaml:"initial-backoff,omitempty"`
	// MaxBackoff controls the maximum duration the client will sleep before retrying a request.
	MaxBackoff *time.Duration `json:"max-backoff,omitempty" yaml:"max-backoff,omitempty"`

	// ConnectTimeout is the maximum time for the net.Dialer to connect to the remote host.
	ConnectTimeout *time.Duration `json:"connect-timeout,omitempty" yaml:"connect-timeout,omitempty"`
	// ReadTimeout is the maximum timeout for non-mutating requests.
	// NOTE: The current implementation uses the max(ReadTimeout, WriteTimeout) to set the http.Client timeout value.
	ReadTimeout *time.Duration `json:"read-timeout,omitempty" yaml:"read-timeout,omitempty"`
	// WriteTimeout is the maximum timeout for mutating requests.
	// NOTE: The current implementation uses the max(ReadTimeout, WriteTimeout) to set the http.Client timeout value.
	WriteTimeout *time.Duration `json:"write-timeout,omitempty" yaml:"write-timeout,omitempty"`
	// IdleConnTimeout sets the timeout for idle connections.
	IdleConnTimeout *time.Duration `json:"idle-conn-timeout,omitempty" yaml:"idle-conn-timeout,omitempty"`
	// TLSHandshakeTimeout sets the timeout for TLS handshakes
	TLSHandshakeTimeout *time.Duration `json:"tls-handshake-timeout,omitempty" yaml:"tls-handshake-timeout,omitempty"`
	// IdleConnTimeout sets the timeout to receive the server's first response headers after
	// fully writing the request headers if the request has an "Expect: 100-continue" header.
	ExpectContinueTimeout *time.Duration `json:"expect-continue-timeout,omitempty" yaml:"expect-continue-timeout,omitempty"`
	// ResponseHeaderTimeout, if non-zero, specifies the amount of time to wait for a server's response headers after fully
	// writing the request (including its body, if any). This time does not include the time to read the response body.
	ResponseHeaderTimeout *time.Duration `json:"response-header-timeout,omitempty" yaml:"response-header-timeout,omitempty"`
	// KeepAlive sets the time to keep idle connections alive.
	// If unset, the client defaults to 30s. If set to 0, the client will not keep connections alive.
	KeepAlive *time.Duration `json:"keep-alive,omitempty" yaml:"keep-alive,omitempty"`

	// HTTP2ReadIdleTimeout sets the maximum time to wait before sending periodic health checks (pings) for an HTTP/2 connection.
	// If unset, the client defaults to 30s for HTTP/2 clients.
	HTTP2ReadIdleTimeout *time.Duration `json:"http2-read-idle-timeout,omitempty" yaml:"http2-read-idle-timeout,omitempty"`
	// HTTP2PingTimeout is the maximum time to wait for a ping response in an HTTP/2 connection,
	// when health checking is enabled which is done by setting the HTTP2ReadIdleTimeout > 0.
	// If unset, the client defaults to 15s if the HTTP2ReadIdleTimeout is > 0.
	HTTP2PingTimeout *time.Duration `json:"http2-ping-timeout,omitempty" yaml:"http2-ping-timeout,omitempty"`

	// MaxIdleConns sets the number of reusable TCP connections the client will maintain.
	// If unset, the client defaults to 200.
	MaxIdleConns *int `json:"max-idle-conns,omitempty" yaml:"max-idle-conns,omitempty"`
	// MaxIdleConnsPerHost sets the number of reusable TCP connections the client will maintain per destination.
	// If unset, the client defaults to 100.
	MaxIdleConnsPerHost *int `json:"max-idle-conns-per-host,omitempty" yaml:"max-idle-conns-per-host,omitempty"`

	// Metrics allows disabling metric emission or adding additional static tags to the client metrics.
	Metrics MetricsConfig `json:"metrics" yaml:"metrics,omitempty"`
	// Security configures the TLS configuration for the client. It accepts file paths which should be
	// absolute paths or relative to the process's current working directory.
	Security SecurityConfig `json:"security" yaml:"security,omitempty"`
}

// BasicAuth represents the configuration for HTTP Basic Authorization
type BasicAuth struct {
	// User is a string representing the user
	User string `json:"user,omitempty" yaml:"user,omitempty"`
	// Password is a string representing the password
	Password string `json:"password,omitempty" yaml:"password,omitempty"`
}

// MetricsConfig configures client metrics emission.
type MetricsConfig struct {
	// Enabled can be used to disable metrics with an explicit 'false'. Metrics are enabled if this is unset.
	Enabled *bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	// Tags allows setting arbitrary additional tags on the metrics emitted by the client.
	Tags map[string]string `json:"tags,omitempty" yaml:"tags,omitempty"`
}

// SecurityConfig configures TLS for the client.
type SecurityConfig struct {
	CAFiles  []string `json:"ca-files,omitempty" yaml:"ca-files,omitempty"`
	CertFile string   `json:"cert-file,omitempty" yaml:"cert-file,omitempty"`
	KeyFile  string   `json:"key-file,omitempty" yaml:"key-file,omitempty"`

	// InsecureSkipVerify sets the InsecureSkipVerify field for the HTTP client's tls config.
	// This option should only be used in clients that have other ways to establish trust with servers.
	InsecureSkipVerify *bool `json:"insecure-skip-verify,omitempty" yaml:"insecure-skip-verify,omitempty"`

	// DynamicCertReload enables re-reading client TLS cert/key files on each TLS handshake.
	// When enabled, rotated certificates are picked up without restarting the process.
	DynamicCertReload *bool `json:"dynamic-cert-reload,omitempty" yaml:"dynamic-cert-reload,omitempty"`
}

// MustClientConfig returns an error if the service name is not configured.
func (c ServicesConfig) MustClientConfig(serviceName string) (ClientConfig, error) {
	if _, ok := c.Services[serviceName]; !ok {
		return ClientConfig{}, werror.Error("ClientConfiguration not found for serviceName", werror.SafeParam("serviceName", serviceName))
	}
	return c.ClientConfig(serviceName), nil
}

// ClientConfig returns the default configuration merged with service-specific configuration.
// If the serviceName is not in the service map, an empty configuration (plus defaults) is used.
func (c ServicesConfig) ClientConfig(serviceName string) ClientConfig {
	conf, ok := c.Services[serviceName]
	if !ok {
		conf = ClientConfig{}
	}
	conf.ServiceName = serviceName

	return MergeClientConfig(conf, c.Default)
}

// MergeClientConfig merges two instances of ClientConfig, preferring values from conf over defaults.
// The ServiceName field is not affected, and is expected to be set in the config before building a Client.
func MergeClientConfig(conf, defaults ClientConfig) ClientConfig {
	if len(conf.URIs) == 0 {
		conf.URIs = defaults.URIs
	}
	if conf.APIToken == nil {
		conf.APIToken = defaults.APIToken
	}
	if conf.APITokenFile == nil {
		conf.APITokenFile = defaults.APITokenFile
	}
	if conf.BasicAuth == nil {
		conf.BasicAuth = defaults.BasicAuth
	}
	if conf.MaxNumRetries == nil {
		conf.MaxNumRetries = defaults.MaxNumRetries
	}
	if conf.ConnectTimeout == nil {
		conf.ConnectTimeout = defaults.ConnectTimeout
	}
	if conf.ReadTimeout == nil {
		conf.ReadTimeout = defaults.ReadTimeout
	}
	if conf.WriteTimeout == nil {
		conf.WriteTimeout = defaults.WriteTimeout
	}
	if conf.IdleConnTimeout == nil {
		conf.IdleConnTimeout = defaults.IdleConnTimeout
	}
	if conf.TLSHandshakeTimeout == nil {
		conf.TLSHandshakeTimeout = defaults.TLSHandshakeTimeout
	}
	if conf.ExpectContinueTimeout == nil {
		conf.ExpectContinueTimeout = defaults.ExpectContinueTimeout
	}
	if conf.ResponseHeaderTimeout == nil {
		conf.ResponseHeaderTimeout = defaults.ResponseHeaderTimeout
	}
	if conf.KeepAlive == nil {
		conf.KeepAlive = defaults.KeepAlive
	}
	if conf.HTTP2ReadIdleTimeout == nil {
		conf.HTTP2ReadIdleTimeout = defaults.HTTP2ReadIdleTimeout
	}
	if conf.HTTP2PingTimeout == nil {
		conf.HTTP2PingTimeout = defaults.HTTP2PingTimeout
	}
	if conf.MaxIdleConns == nil {
		conf.MaxIdleConns = defaults.MaxIdleConns
	}
	if conf.MaxIdleConnsPerHost == nil {
		conf.MaxIdleConnsPerHost = defaults.MaxIdleConnsPerHost
	}
	if conf.Metrics.Enabled == nil {
		conf.Metrics.Enabled = defaults.Metrics.Enabled
	}
	if conf.InitialBackoff == nil {
		conf.InitialBackoff = defaults.InitialBackoff
	}
	if conf.MaxBackoff == nil {
		conf.MaxBackoff = defaults.MaxBackoff
	}
	if conf.DisableHTTP2 == nil {
		conf.DisableHTTP2 = defaults.DisableHTTP2
	}
	if conf.ProxyFromEnvironment == nil {
		conf.ProxyFromEnvironment = defaults.ProxyFromEnvironment
	}
	if conf.ProxyURL == nil {
		conf.ProxyURL = defaults.ProxyURL
	}

	if len(defaults.Metrics.Tags) != 0 {
		if conf.Metrics.Tags == nil {
			conf.Metrics.Tags = make(map[string]string, len(defaults.Metrics.Tags))
		}
		for k, v := range defaults.Metrics.Tags {
			if _, ok := conf.Metrics.Tags[k]; !ok {
				conf.Metrics.Tags[k] = v
			}
		}
	}
	if conf.Security.CAFiles == nil {
		conf.Security.CAFiles = defaults.Security.CAFiles
	}
	if conf.Security.CertFile == "" {
		conf.Security.CertFile = defaults.Security.CertFile
	}
	if conf.Security.KeyFile == "" {
		conf.Security.KeyFile = defaults.Security.KeyFile
	}
	if conf.Security.InsecureSkipVerify == nil {
		conf.Security.InsecureSkipVerify = defaults.Security.InsecureSkipVerify
	}
	if conf.Security.DynamicCertReload == nil {
		conf.Security.DynamicCertReload = defaults.Security.DynamicCertReload
	}
	return conf
}

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
	dynamicCertReload  *bool

	// Retry
	maxAttempts    *int
	initialBackoff *time.Duration
	maxBackoff     *time.Duration

	// Metrics
	disableMetrics *bool
	metricsTags    metrics.Tags
}

// newValidatedClientParams validates a ClientConfig and converts it to
// httpc-native types. Only fields explicitly set in the config are populated;
// unset fields remain at their zero/nil value.
func newValidatedClientParams(config ClientConfig) (validatedClientParams, error) {
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
				return validatedClientParams{}, werror.Wrap(err, "invalid url")
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
			return validatedClientParams{}, werror.Wrap(err, "failed to read api-token-file", werror.SafeParam("file", file))
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
			return validatedClientParams{}, werror.Wrap(err, "invalid proxy url")
		}
		switch proxyURL.Scheme {
		case "http", "https":
			p.httpProxyURL = proxyURL
		case "socks5", "socks5h":
			p.socksProxyURL = proxyURL
		default:
			return validatedClientParams{}, werror.Error("invalid proxy url: only http(s) and socks5 are supported")
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
	p.dynamicCertReload = config.Security.DynamicCertReload

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
		tags, err := metrics.NewTags(config.Metrics.Tags)
		if err != nil {
			return validatedClientParams{}, werror.Wrap(err, "invalid metrics configuration")
		}
		p.metricsTags = tags
	}

	return p, nil
}

// applyValidatedParams calls builder setter methods only for fields that the
// config explicitly specified. Unset fields are left at whatever the builder
// already has, preserving composability with other builder calls.
func applyValidatedParams(b *Builder, p validatedClientParams) {
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
	if p.dynamicCertReload != nil {
		b.SetDynamicCertReload(*p.dynamicCertReload)
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
		staticTags := p.metricsTags
		b.metricsTagProviders = append(b.metricsTagProviders, TagsProviderFunc(func(*http.Request, *http.Response, error) metrics.Tags {
			return staticTags
		}))
	}
}

// ApplyConfig validates the config and calls builder setter methods for each
// field the config explicitly specifies. Fields not present in the config are
// left at whatever the builder already has (from NewBuilder
// defaults or prior setter calls), preserving composability.
// Validation errors are deferred to b.errs (surfaced at Build time).
func (b *Builder) ApplyConfig(config ClientConfig) *Builder {
	params, err := newValidatedClientParams(config)
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
func (b *Builder) ApplyConfigRefreshable(ctx context.Context, config refreshable.Refreshable[ClientConfig]) *Builder {
	// Stage 1: Validate initial config and create validated refreshable.
	validParams, _, err := refreshable.MapWithError(ctx, config, func(ctx context.Context, config ClientConfig) (validatedClientParams, error) {
		return newValidatedClientParams(config)
	})
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
		// Only set cert/key when both are present — a lone cert or key file
		// would cause BuildTLSConfig to try to watch a file that may not exist.
		if p.certFile != "" && p.keyFile != "" {
			t.CertFile = p.certFile
			t.KeyFile = p.keyFile
		}
		if p.insecureSkipVerify != nil {
			t.InsecureSkipVerify = *p.insecureSkipVerify
		}
		if p.dynamicCertReload != nil {
			t.DynamicCertReload = *p.dynamicCertReload
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
	b.metricsTagProviders = append(b.metricsTagProviders, TagsProviderFunc(func(*http.Request, *http.Response, error) metrics.Tags {
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
