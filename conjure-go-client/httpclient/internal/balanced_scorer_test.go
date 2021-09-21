package internal

import (
	"github.com/stretchr/testify/assert"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBalancedScorerRandomizesWithNoneInflight(t *testing.T) {
	uris := []string{"uri1", "uri2", "uri3", "uri4", "uri5"}
	scorer := NewBalancedURIScoringMiddleware(uris, func() int64 { return 0 })
	scoredUris := scorer.GetURIsInOrderOfIncreasingScore()
	assert.ElementsMatch(t, scoredUris, uris)
	assert.NotEqual(t, scoredUris, uris)
}

func TestBalancedScoring(t *testing.T) {
	server200 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))
	defer server200.Close()
	server429 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server429.Close()
	server503 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server503.Close()
	uris := []string{server503.URL, server429.URL, server200.URL}
	scorer := NewBalancedURIScoringMiddleware(uris, func() int64 { return 0 })
	for _, server := range []*httptest.Server{server200, server429, server503} {
		req, err := http.NewRequest("GET", server.URL, nil)
		assert.NoError(t, err)
		_, err = scorer.RoundTrip(req, server.Client().Transport)
		assert.NoError(t, err)
	}
	scoredUris := scorer.GetURIsInOrderOfIncreasingScore()
	assert.Equal(t, scoredUris, []string{server200.URL, server429.URL, server503.URL})
}
