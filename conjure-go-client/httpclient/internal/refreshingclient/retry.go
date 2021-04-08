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

	"github.com/palantir/pkg/refreshable"
	"github.com/palantir/pkg/retry"
)

type RefreshableRetryOptions interface {
	RetryOptions() []retry.Option
}

type RetryParams struct {
	InitialBackoff      *time.Duration
	MaxBackoff          *time.Duration
	Multiplier          *float64
	RandomizationFactor *float64
}

type RefreshableRetryParams struct {
	RefreshableRetryOptions
	refreshable.Refreshable // contains RetryParams
}

// TransformParams accepts a mapping function which will be applied to the params value as it is evaluated.
// This can be used to layer/overwrite configuration before building the RefreshableDialer.
func (r RefreshableRetryParams) TransformParams(mapFn func(p RetryParams) RetryParams) RefreshableRetryParams {
	return RefreshableRetryParams{
		Refreshable: r.Map(func(i interface{}) interface{} {
			return mapFn(i.(RetryParams))
		}),
	}
}

func (r RefreshableRetryParams) RetryOptions() []retry.Option {
	p := r.Current().(RetryParams)
	var opts []retry.Option
	if p.InitialBackoff != nil {
		opts = append(opts, retry.WithInitialBackoff(*p.InitialBackoff))
	}
	if p.MaxBackoff != nil {
		opts = append(opts, retry.WithMaxBackoff(*p.MaxBackoff))
	}
	if p.Multiplier != nil {
		opts = append(opts, retry.WithMultiplier(*p.Multiplier))
	}
	if p.RandomizationFactor != nil {
		opts = append(opts, retry.WithRandomizationFactor(*p.RandomizationFactor))
	}
	return opts
}
