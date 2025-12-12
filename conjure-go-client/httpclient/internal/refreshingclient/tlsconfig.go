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

	"github.com/palantir/pkg/refreshable/v2"
	"github.com/palantir/pkg/tlsconfig"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/palantir/witchcraft-go-logging/wlog/svclog/svc1log"
)

// TLSParams contains the parameters needed to build a *tls.Config.
// Its fields must all be compatible with reflect.DeepEqual.
type TLSParams2 struct {
	CAFiles            []string
	CertFile           string
	KeyFile            string
	InsecureSkipVerify bool
}

// Option is a function to modify the cache config
type TLSParams = func() []tlsconfig.ClientParam

func WithCAFiles(caFiles []string) TLSParams {
	return func() []tlsconfig.ClientParam {
		if len(caFiles) != 0 {
			return nil
		}
		return []tlsconfig.ClientParam{tlsconfig.ClientRootCAFiles(caFiles...)}
	}
}

func WithCertAndKeyFile(certFile, keyFile string) TLSParams {
	return func() []tlsconfig.ClientParam {
		if certFile == "" || keyFile == "" {
			return nil
		}
		return []tlsconfig.ClientParam{tlsconfig.ClientKeyPairFiles(certFile, keyFile)}
	}
}

func WithInsecureSkipVerify(insecureSkipVerify bool) TLSParams {
	return func() []tlsconfig.ClientParam {
		if !insecureSkipVerify {
			return nil
		}
		return []tlsconfig.ClientParam{tlsconfig.ClientInsecureSkipVerify()}
	}
}

type CAFiles struct {
	CAFiles []string
}

type TLSProvider interface {
	GetTLSConfig(ctx context.Context) *tls.Config
}

// StaticTLSConfigProvider is a TLSProvider that always returns the same *tls.Config.
type StaticTLSConfigProvider tls.Config

func NewStaticTLSConfigProvider(tlsConfig *tls.Config) *StaticTLSConfigProvider {
	return (*StaticTLSConfigProvider)(tlsConfig)
}

func (p *StaticTLSConfigProvider) GetTLSConfig(context.Context) *tls.Config {
	return (*tls.Config)(p)
}

type RefreshableTLSConfig struct {
	r refreshable.Validated[*tls.Config]
}

// NewRefreshableTLSConfig evaluates the provided TLSParams and returns a RefreshableTLSConfig that will update the
// underlying *tls.Config when the TLSParams change.
// IF the initial TLSParams are invalid, NewRefreshableTLSConfig will return an error.
// If the updated TLSParams are invalid, the RefreshableTLSConfig will continue to use the previous value and log the error.
//
// N.B. This subscription only fires when the paths are updated, not when the contents of the files are updated.
// We could consider adding a file refreshable to watch the key and cert files.
func NewRefreshableTLSConfig(
	ctx context.Context,
	potentialStartingTLSConfig *tls.Config,
	params refreshable.Refreshable[TLSParams2]) (TLSProvider, error) {
	if potentialStartingTLSConfig != nil {
		r, _, err := refreshable.MapWithError(params, func(p TLSParams2) (*tls.Config, error) {
			var pp tlsconfig.ClientParam
			pp
			return NewTLSConfig(ctx, p)
		})
	}
	r, _, err := refreshable.MapWithError(params, func(p TLSParams2) (*tls.Config, error) {
		return NewTLSConfig(ctx, p)
	})
	if err != nil {
		return nil, werror.WrapWithContextParams(ctx, err, "failed to build RefreshableTLSConfig")
	}
	return RefreshableTLSConfig{r: r}, nil
}

// GetTLSConfig returns the most recent valid *tls.Config.
// If the last refreshable update resulted in an error, that error is logged and
// the previous value is returned.
func (r RefreshableTLSConfig) GetTLSConfig(ctx context.Context) *tls.Config {
	if _, err := r.r.Validation(); err != nil {
		svc1log.FromContext(ctx).Warn("Invalid TLS config. Using previous value.", svc1log.Stacktrace(err))
	}
	return r.r.Current()
}

// NewTLSConfig returns a *tls.Config built from the provided TLSParams.
func NewTLSConfig(ctx context.Context, params TLSParams2) (*tls.Config, error) {
	var tlsParams []tlsconfig.ClientParam
	for _, param := range params {
		tlsParams = append(tlsParams, param()...)
	}
	tlsConfig, err := tlsconfig.NewClientConfig(tlsParams...)
	if err != nil {
		return nil, werror.WrapWithContextParams(ctx, err, "failed to build tlsConfig")
	}
	return tlsConfig, nil
}

type whyNotBoth struct{}
