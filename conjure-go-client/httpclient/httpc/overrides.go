// Copyright (c) 2026 Palantir Technologies. All rights reserved.
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

package httpc

import (
	"net/http"
	"net/url"
	"time"
)

type basicAuthOverride struct {
	user     string
	password string
}

// Overrides is per-request configuration applied to an [Endpoint] via
// [Endpoint.WithOverrides], or embedded in a generated service client struct.
// All methods are copy-on-write, so Overrides is safe to share across
// goroutines.
//
// Headers and query parameters have Set and Add variants: SetHeader/SetQuery
// replaces all values for a key; AddHeader/AddQuery accumulates. Calling
// SetHeader after AddHeader for the same key discards the Add values (Set wins).
type Overrides struct {
	setHeaders   http.Header
	addHeaders   http.Header
	setQuery     url.Values
	addQuery     url.Values
	timeout      *time.Duration
	errorDecoder ErrorDecoder
	basicAuth    *basicAuthOverride
	middlewares  []Middleware
}

// Clone returns a deep copy of the Overrides value.
func (c Overrides) Clone() Overrides {
	out := c
	if c.setHeaders != nil {
		out.setHeaders = c.setHeaders.Clone()
	}
	if c.addHeaders != nil {
		out.addHeaders = c.addHeaders.Clone()
	}
	if c.setQuery != nil {
		cp := make(url.Values, len(c.setQuery))
		for k, v := range c.setQuery {
			cp[k] = append([]string(nil), v...)
		}
		out.setQuery = cp
	}
	if c.addQuery != nil {
		cp := make(url.Values, len(c.addQuery))
		for k, v := range c.addQuery {
			cp[k] = append([]string(nil), v...)
		}
		out.addQuery = cp
	}
	if c.middlewares != nil {
		out.middlewares = make([]Middleware, len(c.middlewares))
		copy(out.middlewares, c.middlewares)
	}
	if c.timeout != nil {
		t := *c.timeout
		out.timeout = &t
	}
	if c.basicAuth != nil {
		ba := *c.basicAuth
		out.basicAuth = &ba
	}
	return out
}

// AddHeader adds a request header. Multiple calls with the same key accumulate values.
func (c Overrides) AddHeader(key, value string) Overrides {
	c = c.Clone()
	if c.addHeaders == nil {
		c.addHeaders = make(http.Header)
	}
	c.addHeaders.Add(key, value)
	return c
}

// SetHeader sets a request header, replacing any previously added or set values for the key.
func (c Overrides) SetHeader(key, value string) Overrides {
	c = c.Clone()
	if c.setHeaders == nil {
		c.setHeaders = make(http.Header)
	}
	c.setHeaders.Set(key, value)
	if c.addHeaders != nil {
		delete(c.addHeaders, http.CanonicalHeaderKey(key))
	}
	return c
}

// AddQuery adds a query parameter. Multiple calls with the same key accumulate values.
func (c Overrides) AddQuery(key, value string) Overrides {
	c = c.Clone()
	if c.addQuery == nil {
		c.addQuery = make(url.Values)
	}
	c.addQuery.Add(key, value)
	return c
}

// SetQuery sets a query parameter, replacing any previously added or set values for the key.
func (c Overrides) SetQuery(key, value string) Overrides {
	c = c.Clone()
	if c.setQuery == nil {
		c.setQuery = make(url.Values)
	}
	c.setQuery.Set(key, value)
	// Set wins over prior Add for the same key.
	if c.addQuery != nil {
		delete(c.addQuery, key)
	}
	return c
}

// WithTimeout sets a per-request timeout that overrides the client-level timeout.
func (c Overrides) WithTimeout(d time.Duration) Overrides {
	c = c.Clone()
	c.timeout = &d
	return c
}

// WithErrorDecoder sets a per-request error decoder that overrides the client-level decoder.
func (c Overrides) WithErrorDecoder(d ErrorDecoder) Overrides {
	c = c.Clone()
	c.errorDecoder = d
	return c
}

// WithBasicAuth sets per-request basic auth credentials, overriding any client-level auth.
func (c Overrides) WithBasicAuth(user, password string) Overrides {
	c = c.Clone()
	c.basicAuth = &basicAuthOverride{user: user, password: password}
	return c
}

// WithMiddleware appends a per-request middleware to the chain.
func (c Overrides) WithMiddleware(m Middleware) Overrides {
	c = c.Clone()
	c.middlewares = append(c.middlewares, m)
	return c
}

// merge combines the receiver with o: set headers/query from o replace and
// clear matching add entries; add headers/query accumulate; timeout, error
// decoder, and basic auth are last-wins (o wins if set); middlewares append.
func (c Overrides) merge(o Overrides) Overrides {
	out := c.Clone()

	for k, vs := range o.setHeaders {
		if out.setHeaders == nil {
			out.setHeaders = make(http.Header)
		}
		out.setHeaders[k] = append([]string(nil), vs...)
		if out.addHeaders != nil {
			delete(out.addHeaders, k)
		}
	}
	for k, vs := range o.addHeaders {
		for _, v := range vs {
			if out.addHeaders == nil {
				out.addHeaders = make(http.Header)
			}
			out.addHeaders.Add(k, v)
		}
	}

	for k, vs := range o.setQuery {
		if out.setQuery == nil {
			out.setQuery = make(url.Values)
		}
		out.setQuery[k] = append([]string(nil), vs...)
		if out.addQuery != nil {
			delete(out.addQuery, k)
		}
	}
	for k, vs := range o.addQuery {
		for _, v := range vs {
			if out.addQuery == nil {
				out.addQuery = make(url.Values)
			}
			out.addQuery.Add(k, v)
		}
	}

	if o.timeout != nil {
		out.timeout = o.timeout
	}
	if o.errorDecoder != nil {
		out.errorDecoder = o.errorDecoder
	}
	if o.basicAuth != nil {
		out.basicAuth = o.basicAuth
	}
	out.middlewares = append(out.middlewares, o.middlewares...)
	return out
}
