package internal

import (
	"net/http"
)

// URISelector is used in combination with a URIPool to get the
// preferred next URL for a given request.
type URISelector interface {
	Select([]string, http.Header) (string, error)
	RoundTrip(*http.Request, http.RoundTripper) (*http.Response, error)
}

// URIPool stores all possible URIs for a given connection. It can be use
// as http middleware in order to maintain state between requests.
type URIPool interface {
	// NumURIs returns the overall count of URIs available in the
	// connection pool.
	NumURIs() int
	// URIs returns the set of URIs that should be considered by a
	// URISelector
	URIs() []string
	RoundTrip(*http.Request, http.RoundTripper) (*http.Response, error)
}
