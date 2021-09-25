package internal

import (
	"github.com/palantir/pkg/refreshable"
)

type RefreshableURIScoringMiddleware interface {
	CurrentURIScoringMiddleware() URIScoringMiddleware
}

func NewRefreshableURIScoringMiddleware(uris refreshable.StringSlice, constructor func([]string) URIScoringMiddleware) RefreshableURIScoringMiddleware {
	return refreshableURIScoringMiddleware{uris.MapStringSlice(func(uris []string) interface{} {
		return constructor(uris)
	})}
}

type refreshableURIScoringMiddleware struct{ refreshable.Refreshable }

func (r refreshableURIScoringMiddleware) CurrentURIScoringMiddleware() URIScoringMiddleware {
	return r.Current().(URIScoringMiddleware)
}
