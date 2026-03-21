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
// or embedded in a generated service client struct. All With* methods use
// copy-on-write semantics: they return a new Overrides value without modifying
// the original.
type Overrides struct {
	headers      http.Header
	queryParams  url.Values
	timeout      *time.Duration
	errorDecoder ErrorDecoder
	basicAuth    *basicAuthOverride
	middlewares  []Middleware
}

// Clone returns a deep copy of the Overrides value.
func (c Overrides) Clone() Overrides {
	out := c
	if c.headers != nil {
		out.headers = c.headers.Clone()
	}
	if c.queryParams != nil {
		cp := make(url.Values, len(c.queryParams))
		for k, v := range c.queryParams {
			cp[k] = append([]string(nil), v...)
		}
		out.queryParams = cp
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

// WithHeader adds a request header. Multiple calls with the same key accumulate values.
// Returns a new Overrides value; the original is unchanged.
func (c Overrides) WithHeader(key, value string) Overrides {
	c = c.Clone()
	if c.headers == nil {
		c.headers = make(http.Header)
	}
	c.headers.Add(key, value)
	return c
}

// WithQueryParam adds a query parameter. Multiple calls with the same key accumulate values.
// Returns a new Overrides value; the original is unchanged.
func (c Overrides) WithQueryParam(key, value string) Overrides {
	c = c.Clone()
	if c.queryParams == nil {
		c.queryParams = make(url.Values)
	}
	c.queryParams.Add(key, value)
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
// Headers and query params are additive. Timeout, error decoder, and basic auth
// use last-wins (o takes precedence if set). Middlewares are appended.
func (c Overrides) merge(o Overrides) Overrides {
	out := c.Clone()
	for k, vs := range o.headers {
		for _, v := range vs {
			if out.headers == nil {
				out.headers = make(http.Header)
			}
			out.headers.Add(k, v)
		}
	}
	for k, vs := range o.queryParams {
		for _, v := range vs {
			if out.queryParams == nil {
				out.queryParams = make(url.Values)
			}
			out.queryParams.Add(k, v)
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
