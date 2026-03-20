package httpc

// baseBuilder is the shared contract for all builder interfaces in this package.
//
// Builders are mutable: setter methods modify the receiver and return it for fluent
// chaining. Clone returns an independent deep copy for forking a configuration;
// mutations to the clone do not affect the original, and vice versa.
//
// Apply applies Param functions to the builder in sequence. Because builders are
// mutable, Apply modifies the receiver in place and returns it. This means that
// after b2 := b1.Apply(p), b1 and b2 refer to the same (now-modified) builder.
// To create a genuinely independent variant, clone first: b2 := b1.Clone().Apply(p).
type baseBuilder[B baseBuilder[B]] interface {
	Clone() B
	Apply(...Param[B]) B
}

// Param is a reusable, composable configuration function for a builder or service client.
// Params are applied via the Apply method and typically wrap one or more setter calls:
//
//	func WithDefaults[B httpc.ClientBuilder[B]]() httpc.Param[B] {
//	    return func(b B) B {
//	        return b.SetTimeout(30 * time.Second).SetMaxRetries(3)
//	    }
//	}
//
// Param is also the option type for ServiceClient, where it wraps copy-on-write
// RequestOverrides methods rather than mutating setters. See ServiceClient for details.
type Param[Self baseBuilder[Self]] func(Self) Self

// ClientBuilder is the top-level builder interface for constructing HTTP clients.
// It composes DialerBuilder, TLSConfigBuilder, TransportBuilder, and ServiceBuilder
// into a single unified builder covering all layers of the HTTP stack.
//
// Like all builders in this package, ClientBuilder is mutable: setter methods modify
// the receiver and return it for chaining. Use Clone to fork an independent copy.
//
// The Build method (inherited from ServiceBuilder) constructs the client by:
//  1. Building a net.Dialer from DialerBuilder settings
//  2. Building a *tls.Config from TLSConfigBuilder settings
//  3. Building an *http.Transport from TransportBuilder settings + dialer + TLS
//  4. Injecting the transport via SetTransport
//  5. Wrapping with the middleware stack and returning a Client
//
// Generic configuration functions can accept any ClientBuilder and return the
// same concrete type, enabling reusable configuration libraries:
//
//	func ApplyDefaults[B ClientBuilder[B]](b B) B {
//	    return b.SetTimeout(30 * time.Second).SetMaxRetries(3)
//	}
type ClientBuilder[B ClientBuilder[B]] interface {
	DialerBuilder[B]
	TLSConfigBuilder[B]
	TransportBuilder[B]
	ServiceBuilder[B]
}
