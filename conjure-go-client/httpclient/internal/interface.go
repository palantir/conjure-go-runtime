// Copyright (c) 2022 Palantir Technologies. All rights reserved.
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
