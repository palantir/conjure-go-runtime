package httpc

import (
	"net/http"
	"net/url"
	"time"
)

// basicAuthOverride holds basic auth credentials for per-request overrides.
type basicAuthOverride struct {
	user     string
	password string
}

// Overrides holds per-request configuration that can be applied to an Endpoint
// or embedded in a generated service client struct. All methods use
// copy-on-write semantics: they return a new Overrides value without modifying
// the original.
//
// Headers and query parameters have both Set and Add variants:
//   - SetHeader/SetQuery replaces all values for a key (last-wins semantics).
//   - AddHeader/AddQuery accumulates values for a key.
//
// When both Set and Add are used for the same key, Set takes precedence:
// calling SetHeader after AddHeader for the same key discards the Add values.
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
// Returns a new Overrides value; the original is unchanged.
func (c Overrides) AddHeader(key, value string) Overrides {
	c = c.Clone()
	if c.addHeaders == nil {
		c.addHeaders = make(http.Header)
	}
	c.addHeaders.Add(key, value)
	return c
}

// SetHeader sets a request header, replacing any previously added or set values for the key.
// Returns a new Overrides value; the original is unchanged.
func (c Overrides) SetHeader(key, value string) Overrides {
	c = c.Clone()
	if c.setHeaders == nil {
		c.setHeaders = make(http.Header)
	}
	c.setHeaders.Set(key, value)
	// Set wins over prior Add for the same key.
	if c.addHeaders != nil {
		delete(c.addHeaders, http.CanonicalHeaderKey(key))
	}
	return c
}

// AddQuery adds a query parameter. Multiple calls with the same key accumulate values.
// Returns a new Overrides value; the original is unchanged.
func (c Overrides) AddQuery(key, value string) Overrides {
	c = c.Clone()
	if c.addQuery == nil {
		c.addQuery = make(url.Values)
	}
	c.addQuery.Add(key, value)
	return c
}

// SetQuery sets a query parameter, replacing any previously added or set values for the key.
// Returns a new Overrides value; the original is unchanged.
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
// Returns a new Overrides value; the original is unchanged.
func (c Overrides) WithTimeout(d time.Duration) Overrides {
	c = c.Clone()
	c.timeout = &d
	return c
}

// WithErrorDecoder sets a per-request error decoder that overrides the client-level decoder.
// Returns a new Overrides value; the original is unchanged.
func (c Overrides) WithErrorDecoder(d ErrorDecoder) Overrides {
	c = c.Clone()
	c.errorDecoder = d
	return c
}

// WithBasicAuth sets per-request basic auth credentials, overriding any client-level auth.
// Returns a new Overrides value; the original is unchanged.
func (c Overrides) WithBasicAuth(user, password string) Overrides {
	c = c.Clone()
	c.basicAuth = &basicAuthOverride{user: user, password: password}
	return c
}

// WithMiddleware appends a per-request middleware to the chain.
// Returns a new Overrides value; the original is unchanged.
func (c Overrides) WithMiddleware(m Middleware) Overrides {
	c = c.Clone()
	c.middlewares = append(c.middlewares, m)
	return c
}

// merge returns a new Overrides that combines the receiver with o.
//
// Set headers/query from o replace the receiver's values for matching keys and
// delete those keys from the receiver's add maps. Add headers/query from o
// accumulate into the receiver's add maps.
//
// Timeout, error decoder, and basic auth use last-wins (o takes precedence if set).
// Middlewares are appended.
func (c Overrides) merge(o Overrides) Overrides {
	out := c.Clone()

	// Merge set headers: o's set headers replace receiver's set headers and clear add headers for those keys.
	for k, vs := range o.setHeaders {
		if out.setHeaders == nil {
			out.setHeaders = make(http.Header)
		}
		out.setHeaders[k] = append([]string(nil), vs...)
		if out.addHeaders != nil {
			delete(out.addHeaders, k)
		}
	}

	// Merge add headers: accumulate into receiver's add headers.
	for k, vs := range o.addHeaders {
		for _, v := range vs {
			if out.addHeaders == nil {
				out.addHeaders = make(http.Header)
			}
			out.addHeaders.Add(k, v)
		}
	}

	// Merge set query: o's set query replaces receiver's set query and clears add query for those keys.
	for k, vs := range o.setQuery {
		if out.setQuery == nil {
			out.setQuery = make(url.Values)
		}
		out.setQuery[k] = append([]string(nil), vs...)
		if out.addQuery != nil {
			delete(out.addQuery, k)
		}
	}

	// Merge add query: accumulate into receiver's add query.
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
