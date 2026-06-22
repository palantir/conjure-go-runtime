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
	"context"
	"net/http"
	"time"

	"github.com/palantir/pkg/metrics"
	"github.com/palantir/pkg/refreshable/v2"
	werror "github.com/palantir/witchcraft-go-error"
)

// ApplyConfig applies each field config explicitly sets; unset fields leave
// the builder's existing value unchanged. Validation errors are deferred until
// Build.
//
// The internal SetServiceName/SetBaseURLs/... calls below are not virtual: they
// dispatch to the [BuilderCore] method, never a leaf override (Go has no virtual
// dispatch). A downstream leaf therefore cannot intercept config application by
// shadowing a setter.
func (b *BuilderCore[Self]) ApplyConfig(ctx context.Context, config ClientConfig) Self {
	params, err := newValidatedClientParams(ctx, config)
	if err != nil {
		b.errs.setField(fieldConfig, werror.WrapWithContextParams(ctx, err, "invalid client config"))
		return b.self
	}
	b.errs.clearField(fieldConfig)
	if params.serviceName != "" {
		b.SetServiceName(params.serviceName)
	}
	if params.uris != nil {
		b.SetBaseURLs(params.uris...)
	}
	// Only install the authorizer the config explicitly specifies.
	if params.apiToken != nil {
		b.SetAuthToken(*params.apiToken)
	} else if params.basicAuth != nil {
		b.SetBasicAuth(params.basicAuth.User, params.basicAuth.Password)
	}
	if params.timeout != nil {
		b.SetTimeout(*params.timeout)
	}
	if params.connectTimeout != nil {
		b.SetDialTimeout(*params.connectTimeout)
	}
	if params.keepAlive != nil {
		b.SetKeepAlive(*params.keepAlive)
	}
	if params.socksProxyURL != nil {
		b.SetSocksProxyURL(params.socksProxyURL.String())
	}
	if params.httpProxyURL != nil {
		b.SetHTTPProxyURL(params.httpProxyURL.String())
	}
	if params.proxyFromEnvironment != nil {
		if *params.proxyFromEnvironment {
			b.SetProxyFromEnvironment()
		} else {
			b.transportParams = refreshable.View(b.transportParams, func(tp transportParams) transportParams {
				tp.ProxyFromEnvironment = false
				return tp
			})
		}
	}
	if params.maxIdleConns != nil {
		b.SetMaxIdleConns(*params.maxIdleConns)
	}
	if params.maxIdleConnsPerHost != nil {
		b.SetMaxIdleConnsPerHost(*params.maxIdleConnsPerHost)
	}
	if params.disableHTTP2 != nil && *params.disableHTTP2 {
		b.DisableHTTP2()
	}
	if params.idleConnTimeout != nil {
		b.SetIdleConnTimeout(*params.idleConnTimeout)
	}
	if params.expectContinueTimeout != nil {
		b.SetExpectContinueTimeout(*params.expectContinueTimeout)
	}
	if params.responseHeaderTimeout != nil {
		b.SetResponseHeaderTimeout(*params.responseHeaderTimeout)
	}
	if params.tlsHandshakeTimeout != nil {
		b.SetTLSHandshakeTimeout(*params.tlsHandshakeTimeout)
	}
	if params.http2ReadIdleTimeout != nil {
		b.SetHTTP2ReadIdleTimeout(*params.http2ReadIdleTimeout)
	}
	if params.http2PingTimeout != nil {
		b.SetHTTP2PingTimeout(*params.http2PingTimeout)
	}
	if len(params.caFiles) > 0 {
		b.AddCACertFiles(params.caFiles...)
	}
	if params.certFile != "" && params.keyFile != "" {
		b.SetClientCertFiles(params.certFile, params.keyFile)
	}
	if params.insecureSkipVerify != nil {
		b.SetInsecureSkipVerify(*params.insecureSkipVerify)
	}
	if params.dynamicCertReload != nil {
		b.SetDynamicCertReload(*params.dynamicCertReload)
	}
	if params.maxAttempts != nil {
		b.SetMaxAttempts(params.maxAttempts)
	}
	if params.initialBackoff != nil {
		b.SetInitialBackoff(*params.initialBackoff)
	}
	if params.maxBackoff != nil {
		b.SetMaxBackoff(*params.maxBackoff)
	}
	if params.disableMetrics != nil {
		b.SetDisableMetrics(*params.disableMetrics)
	}
	if len(params.metricsTags) > 0 {
		b.metricsTagProviders = append(b.metricsTagProviders, StaticTagsProvider(params.metricsTags))
	}
	return b.self
}

// ApplyServicesConfig looks up the merged [ClientConfig] for serviceName via
// [ServicesConfig.ClientConfig] and applies it. Equivalent to
// b.ApplyConfig(ctx, services.ClientConfig(serviceName)).
func (b *BuilderCore[Self]) ApplyServicesConfig(ctx context.Context, services ServicesConfig, serviceName string) Self {
	return b.ApplyConfig(ctx, services.ClientConfig(serviceName))
}

// ApplyServicesConfigRefreshable is the refreshable analog of
// [Builder.ApplyServicesConfig]: it derives a refreshable [ClientConfig] for
// serviceName and applies it via [Builder.ApplyConfigRefreshable].
func (b *BuilderCore[Self]) ApplyServicesConfigRefreshable(ctx context.Context, services refreshable.Refreshable[ServicesConfig], serviceName string) Self {
	clientConfig := refreshable.MapAuto(services, func(s ServicesConfig) ClientConfig {
		return s.ClientConfig(serviceName)
	})
	return b.ApplyConfigRefreshable(ctx, clientConfig)
}

// ApplyConfigRefreshable wires up refreshable overlays for each field the
// config sets; unset fields fall back to the builder's value at call time.
// The config validation error is deferred until Build and re-evaluated against
// the live config, so a source that is initially invalid but becomes valid
// before Build no longer fails. While invalid, the overlays fall back to the
// builder's pre-config values.
//
// Caveat: subsequent calls to static SetAuthToken / SetBasicAuth (and other
// static SetFoo setters) replace the refreshable wiring for that field. To
// avoid losing dynamic updates, call refreshable-aware setters first or set
// final values on the builder before applying refreshable config.
func (b *BuilderCore[Self]) ApplyConfigRefreshable(ctx context.Context, config refreshable.Refreshable[ClientConfig]) Self {
	validParams, _ := refreshable.MapWithErrorAuto(ctx, config, newValidatedClientParams)
	b.errs.setFieldProvider(fieldConfig, validatedBuilderError(validParams))

	// For each field, install a refreshable that overlays the config value
	// (when set) on top of the builder's existing value (captured now).
	existingServiceName := b.serviceName
	b.serviceName = refreshable.MapFromValidatedAuto(validParams, func(p validatedClientParams) string {
		if p.serviceName != "" {
			return p.serviceName
		}
		return existingServiceName.Current()
	})

	existingURIs := b.uris
	uris := refreshable.MapFromValidatedAuto(validParams, func(p validatedClientParams) []string {
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
	b.timeout = refreshable.MapFromValidatedAuto(validParams, func(p validatedClientParams) time.Duration {
		if p.timeout != nil {
			return *p.timeout
		}
		return existingTimeout.Current()
	})

	existingDialer := b.dialerParams
	b.dialerParams = refreshable.MapFromValidatedAuto(validParams, func(p validatedClientParams) dialerParams {
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

	existingTransport := b.transportParams
	b.transportParams = refreshable.MapFromValidatedAuto(validParams, func(p validatedClientParams) transportParams {
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

	existingTLS := b.tlsFileParams
	b.tlsFileParams = refreshable.MapFromValidatedAuto(validParams, func(p validatedClientParams) tlsFileParams {
		t := existingTLS.Current()
		if len(p.caFiles) > 0 {
			t.CAFiles = p.caFiles
		}
		// Set cert/key only when both are present; a lone path would make
		// BuildTLSConfig watch a file that may not exist.
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
	b.maxAttempts = refreshable.MapFromValidatedAuto(validParams, func(p validatedClientParams) *int {
		if p.maxAttempts != nil {
			return p.maxAttempts
		}
		if existingMaxAttempts != nil {
			return existingMaxAttempts.Current()
		}
		return nil
	})

	existingInitialBackoff := b.initialBackoff
	b.initialBackoff = refreshable.MapFromValidatedAuto(validParams, func(p validatedClientParams) time.Duration {
		if p.initialBackoff != nil {
			return *p.initialBackoff
		}
		return existingInitialBackoff.Current()
	})

	existingMaxBackoff := b.maxBackoff
	b.maxBackoff = refreshable.MapFromValidatedAuto(validParams, func(p validatedClientParams) time.Duration {
		if p.maxBackoff != nil {
			return *p.maxBackoff
		}
		return existingMaxBackoff.Current()
	})

	existingDisableMetrics := b.disableMetrics
	b.disableMetrics = refreshable.MapFromValidatedAuto(validParams, func(p validatedClientParams) bool {
		if p.disableMetrics != nil {
			return *p.disableMetrics
		}
		return existingDisableMetrics.Current()
	})

	// One auth provider covers both token and basic auth so that refreshes can
	// switch between them. When neither is set, fall back to any provider
	// installed by a prior SetAuth* call.
	existingAuth := b.auth
	apiToken := refreshable.MapFromValidatedAuto(validParams, func(p validatedClientParams) *string { return p.apiToken })
	basicAuthR := refreshable.MapFromValidatedAuto(validParams, func(p validatedClientParams) *BasicAuth { return p.basicAuth })
	b.auth = AuthorizerFunc(func(ctx context.Context) (string, error) {
		if s := apiToken.Current(); s != nil {
			return bearerAuthHeader(*s), nil
		}
		if auth := basicAuthR.Current(); auth != nil {
			return basicAuthHeader(auth.User, auth.Password), nil
		}
		if existingAuth != nil {
			return existingAuth.AuthorizationHeader(ctx)
		}
		return "", nil
	})

	b.metricsTagProviders = append(b.metricsTagProviders, TagsProviderFunc(func(*http.Request, *http.Response, error) metrics.Tags {
		return validParams.Unvalidated().metricsTags
	}))

	return b.self
}
