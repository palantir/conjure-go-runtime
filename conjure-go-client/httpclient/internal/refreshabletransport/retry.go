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

package refreshabletransport

import (
	"github.com/palantir/pkg/refreshable"
	"github.com/palantir/pkg/retry"
)

type RefreshableRetryOptions interface {
	CurrentRetryOptions() []retry.Option
}

type RetryParams struct {
	InitialBackoff      refreshable.Duration
	MaxBackoff          refreshable.Duration
	Multiplier          refreshable.Float64
	RandomizationFactor refreshable.Float64
}

func (p *RetryParams) CurrentRetryOptions() []retry.Option {
	var opts []retry.Option
	if p.InitialBackoff != nil {
		opts = append(opts, retry.WithInitialBackoff(p.InitialBackoff.CurrentDuration()))
	}
	if p.MaxBackoff != nil {
		opts = append(opts, retry.WithMaxBackoff(p.MaxBackoff.CurrentDuration()))
	}
	if p.Multiplier != nil {
		opts = append(opts, retry.WithMultiplier(p.Multiplier.CurrentFloat64()))
	}
	if p.RandomizationFactor != nil {
		opts = append(opts, retry.WithRandomizationFactor(p.RandomizationFactor.CurrentFloat64()))
	}
	return opts
}
