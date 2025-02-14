// Copyright (c) 2024 Palantir Technologies. All rights reserved.
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

package refreshingclient

import (
	"context"
	"crypto/tls"
	"sync"
	"sync/atomic"

	"github.com/palantir/pkg/refreshable"
	"github.com/palantir/pkg/tlsconfig"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/palantir/witchcraft-go-logging/wlog/svclog/svc1log"
)

type RefreshableTLSConfig interface {
	GetTLSConfig(ctx context.Context) *tls.Config
	SubscribeToTLSConfig(consumer func(*tls.Config)) (unsubscribe func())
}

var _ RefreshableTLSConfig = (*MappedRefreshableTLSConfig)(nil)

func ConfigureTLSConfig(r RefreshableTLSConfig, mapFn func(conf *tls.Config) *tls.Config) RefreshableTLSConfig {
	var m MappedRefreshableTLSConfig
	r.SubscribeToTLSConfig(func(c *tls.Config) {
		m.Update(mapFn(c))
	})
	return &m
}

type MappedRefreshableTLSConfig struct {
	conf atomic.Pointer[tls.Config]

	mu          sync.Mutex // protects subscribers
	subscribers []*func(*tls.Config)
}

// NewMappedRefreshableTLSConfig returns a new *MappedRefreshableTLSConfig.
func NewMappedRefreshableTLSConfig(conf *tls.Config) *MappedRefreshableTLSConfig {
	var m MappedRefreshableTLSConfig
	m.conf.Store(conf)
	return &m
}

// GetTLSConfig implements RefreshableTLSConf.
func (m *MappedRefreshableTLSConfig) GetTLSConfig(ctx context.Context) *tls.Config {
	return m.conf.Load()
}

func (m *MappedRefreshableTLSConfig) Update(conf *tls.Config) {
	m.conf.Store(conf)

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sub := range m.subscribers {
		(*sub)(conf)
	}
}

// SubscribeToTLSConfig implements RefreshableTLSConf.
func (m *MappedRefreshableTLSConfig) SubscribeToTLSConfig(consumer func(*tls.Config)) (unsubscribe func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	consumerFnPtr := &consumer
	m.subscribers = append(m.subscribers, consumerFnPtr)
	return func() {
		m.unsubscribe(consumerFnPtr)
	}
}

func (m *MappedRefreshableTLSConfig) unsubscribe(consumerFnPtr *func(*tls.Config)) {
	m.mu.Lock()
	defer m.mu.Unlock()

	matchIdx := -1
	for idx, currSub := range m.subscribers {
		if currSub == consumerFnPtr {
			matchIdx = idx
			break
		}
	}
	if matchIdx != -1 {
		m.subscribers = append(m.subscribers[:matchIdx], m.subscribers[matchIdx+1:]...)
	}
}

// TLSParams contains the parameters needed to build a *tls.Config.
// Its fields must all be compatible with reflect.DeepEqual.
type TLSParams struct {
	CAFiles            []string
	CertFile           string
	KeyFile            string
	InsecureSkipVerify bool
}

type TLSProvider interface {
	GetTLSConfig(ctx context.Context) *tls.Config
}

var _ RefreshableTLSConfig = (*StaticTLSConfigProvider)(nil)

// StaticTLSConfigProvider is a TLSProvider that always returns the same *tls.Config.
type StaticTLSConfigProvider tls.Config

func NewStaticTLSConfigProvider(tlsConfig *tls.Config) *StaticTLSConfigProvider {
	return (*StaticTLSConfigProvider)(tlsConfig)
}

func (p *StaticTLSConfigProvider) GetTLSConfig(context.Context) *tls.Config {
	return (*tls.Config)(p)
}

// SubscribeToTLSConfig implements RefreshableTLSConf.
func (p *StaticTLSConfigProvider) SubscribeToTLSConfig(consumer func(*tls.Config)) (unsubscribe func()) {
	return nil
}

type WrappedRefreshableTLSConfig struct {
	r *refreshable.ValidatingRefreshable // contains *tls.Config
}

// NewRefreshableTLSConfigFromParams evaluates the provided TLSParams and returns a RefreshableTLSConfig that will update the
// underlying *tls.Config when the TLSParams change.
// IF the initial TLSParams are invalid, NewRefreshableTLSConfigFromParams will return an error.
// If the updated TLSParams are invalid, the RefreshableTLSConfig will continue to use the previous value and log the error.
//
// N.B. This subscription only fires when the paths are updated, not when the contents of the files are updated.
// We could consider adding a file refreshable to watch the key and cert files.
func NewRefreshableTLSConfigFromParams(ctx context.Context, params RefreshableTLSParams) (RefreshableTLSConfig, error) {
	r, err := refreshable.NewMapValidatingRefreshable(params, func(i interface{}) (interface{}, error) {
		return NewTLSConfig(ctx, i.(TLSParams))
	})
	if err != nil {
		return nil, werror.WrapWithContextParams(ctx, err, "failed to build RefreshableTLSConfig")
	}
	return WrappedRefreshableTLSConfig{r: r}, nil
}

// GetTLSConfig returns the most recent valid *tls.Config.
// If the last refreshable update resulted in an error, that error is logged and
// the previous value is returned.
func (r WrappedRefreshableTLSConfig) GetTLSConfig(ctx context.Context) *tls.Config {
	if err := r.r.LastValidateErr(); err != nil {
		svc1log.FromContext(ctx).Warn("Invalid TLS config. Using previous value.", svc1log.Stacktrace(err))
	}
	return r.r.Current().(*tls.Config)
}

// SubscribeToTLSConfig implements RefreshableTLSConf.
func (r WrappedRefreshableTLSConfig) SubscribeToTLSConfig(consumer func(*tls.Config)) (unsubscribe func()) {
	return r.r.Subscribe(func(i interface{}) {
		consumer(i.(*tls.Config))
	})
}

// NewTLSConfig returns a *tls.Config built from the provided TLSParams.
func NewTLSConfig(ctx context.Context, p TLSParams) (*tls.Config, error) {
	var tlsParams []tlsconfig.ClientParam
	if len(p.CAFiles) != 0 {
		tlsParams = append(tlsParams, tlsconfig.ClientRootCAFiles(p.CAFiles...))
	}
	if p.CertFile != "" && p.KeyFile != "" {
		tlsParams = append(tlsParams, tlsconfig.ClientKeyPairFiles(p.CertFile, p.KeyFile))
	}
	if p.InsecureSkipVerify {
		tlsParams = append(tlsParams, tlsconfig.ClientInsecureSkipVerify())
	}
	tlsConfig, err := tlsconfig.NewClientConfig(tlsParams...)
	if err != nil {
		return nil, werror.WrapWithContextParams(ctx, err, "failed to build tlsConfig")
	}
	return tlsConfig, nil
}
