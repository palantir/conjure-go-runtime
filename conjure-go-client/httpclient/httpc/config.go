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
	"net/url"
	"os"
	"slices"
	"time"

	"github.com/palantir/pkg/metrics"
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
	// ExpectContinueTimeout sets the timeout to receive the server's first response headers after
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
	mergeOptionalConfigSlice(&conf.URIs, defaults.URIs)
	mergeOptionalConfigField(&conf.APIToken, defaults.APIToken)
	mergeOptionalConfigField(&conf.APITokenFile, defaults.APITokenFile)
	mergeOptionalConfigField(&conf.BasicAuth, defaults.BasicAuth)
	mergeOptionalConfigField(&conf.DisableHTTP2, defaults.DisableHTTP2)
	mergeOptionalConfigField(&conf.ProxyFromEnvironment, defaults.ProxyFromEnvironment)
	mergeOptionalConfigField(&conf.ProxyURL, defaults.ProxyURL)
	mergeOptionalConfigField(&conf.MaxNumRetries, defaults.MaxNumRetries)
	mergeOptionalConfigField(&conf.InitialBackoff, defaults.InitialBackoff)
	mergeOptionalConfigField(&conf.MaxBackoff, defaults.MaxBackoff)
	mergeOptionalConfigField(&conf.ConnectTimeout, defaults.ConnectTimeout)
	mergeOptionalConfigField(&conf.ReadTimeout, defaults.ReadTimeout)
	mergeOptionalConfigField(&conf.WriteTimeout, defaults.WriteTimeout)
	mergeOptionalConfigField(&conf.IdleConnTimeout, defaults.IdleConnTimeout)
	mergeOptionalConfigField(&conf.TLSHandshakeTimeout, defaults.TLSHandshakeTimeout)
	mergeOptionalConfigField(&conf.ExpectContinueTimeout, defaults.ExpectContinueTimeout)
	mergeOptionalConfigField(&conf.ResponseHeaderTimeout, defaults.ResponseHeaderTimeout)
	mergeOptionalConfigField(&conf.KeepAlive, defaults.KeepAlive)
	mergeOptionalConfigField(&conf.HTTP2ReadIdleTimeout, defaults.HTTP2ReadIdleTimeout)
	mergeOptionalConfigField(&conf.HTTP2PingTimeout, defaults.HTTP2PingTimeout)
	mergeOptionalConfigField(&conf.MaxIdleConns, defaults.MaxIdleConns)
	mergeOptionalConfigField(&conf.MaxIdleConnsPerHost, defaults.MaxIdleConnsPerHost)
	mergeOptionalConfigField(&conf.Metrics.Enabled, defaults.Metrics.Enabled)
	mergeOptionalConfigField(&conf.Security.InsecureSkipVerify, defaults.Security.InsecureSkipVerify)
	mergeOptionalConfigField(&conf.Security.DynamicCertReload, defaults.Security.DynamicCertReload)
	mergeOptionalConfigSlice(&conf.Security.CAFiles, defaults.Security.CAFiles)

	if conf.Security.CertFile == "" {
		conf.Security.CertFile = defaults.Security.CertFile
	}
	if conf.Security.KeyFile == "" {
		conf.Security.KeyFile = defaults.Security.KeyFile
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
	return conf
}

func mergeOptionalConfigField[T any](conf **T, defaults *T) {
	if *conf == nil {
		*conf = defaults
	}
}

func mergeOptionalConfigSlice[T any](conf *[]T, defaults []T) {
	if len(*conf) == 0 {
		*conf = defaults
	}
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
	if config.ReadTimeout != nil && config.WriteTimeout != nil {
		p.timeout = new(max(*config.ReadTimeout, *config.WriteTimeout))
	} else if config.ReadTimeout != nil {
		p.timeout = new(*config.ReadTimeout)
	} else if config.WriteTimeout != nil {
		p.timeout = new(*config.WriteTimeout)
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
		p.disableMetrics = new(!*config.Metrics.Enabled)
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
