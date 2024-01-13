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

// URISelector is middleware implemented on a CGR client to determine available/prefered URIs to use when building
// requests.
type URISelector interface {
	// Select takes a list of URIs and returns all or a subset of the the passed in URIs ordered by preference of
	// use.
	Select([]string, http.Header) ([]string, error)
	// RoundTrip implements CGR middleware
	RoundTrip(*http.Request, http.RoundTripper) (*http.Response, error)
}
