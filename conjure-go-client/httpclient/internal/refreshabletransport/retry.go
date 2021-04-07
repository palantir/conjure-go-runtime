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
