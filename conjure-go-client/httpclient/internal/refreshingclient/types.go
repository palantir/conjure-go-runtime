// Copyright (c) 2021 Palantir Technologies. All rights reserved.
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
	"time"

	"github.com/palantir/pkg/metrics"
	"github.com/palantir/pkg/refreshable/v2"
)

// ValidatedClientParams represents a set of fields derived from a snapshot of ClientConfig.
// It is designed for use within a refreshable: fields are comparable with reflect.DeepEqual
// so unnecessary updates are not pushed to subscribers.
// Values are generally known to be "valid" to minimize downstream error handling.
type ValidatedClientParams struct {
	APIToken       *string
	BasicAuth      *BasicAuth
	Dialer         DialerParams
	DisableMetrics bool
	MaxAttempts    *int
	MetricsTags    metrics.Tags
	Retry          RetryParams
	Timeout        time.Duration
	Transport      TransportParams
	URIs           []string
}

// BasicAuth represents the configuration for HTTP Basic Authorization
type BasicAuth struct {
	User     string
	Password string
}

func (p ValidatedClientParams) GetAPIToken() *string          { return p.APIToken }
func (p ValidatedClientParams) GetBasicAuth() *BasicAuth      { return p.BasicAuth }
func (p ValidatedClientParams) GetDialerParams() DialerParams { return p.Dialer }
func (p ValidatedClientParams) GetDisableMetrics() bool       { return p.DisableMetrics }
func (p ValidatedClientParams) GetMaxAttempts() *int          { return p.MaxAttempts }
func (p ValidatedClientParams) GetMetricsTags() metrics.Tags  { return p.MetricsTags }
func (p ValidatedClientParams) GetRetry() RetryParams         { return p.Retry }
func (p ValidatedClientParams) GetTimeout() time.Duration     { return p.Timeout }
func (p ValidatedClientParams) GetTransport() TransportParams { return p.Transport }
func (p ValidatedClientParams) GetURIs() []string             { return p.URIs }

func MapValidClientParams[T any](
	r refreshable.Refreshable[ValidatedClientParams],
	mapFn func(val ValidatedClientParams) T,
) refreshable.Refreshable[T] {
	out, _ := refreshable.Map[ValidatedClientParams, T](r, mapFn)
	return out
}
