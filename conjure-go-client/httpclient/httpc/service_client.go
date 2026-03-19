package httpc

import "time"

// ServiceClient is the interface that generated Conjure service clients implement.
// It combines Clone and Apply for configuration management with RequestOverrides
// for per-request customization.
//
// Unlike the mutable builder interfaces, ServiceClient uses copy-on-write semantics:
// all methods (Clone, Apply, and the inherited RequestOverrides With* methods) return
// a new client value without modifying the original. This makes it safe to store a
// base client and derive customized copies concurrently:
//
//	base := NewMyServiceClient(httpClient)
//	custom := base.WithHeader("X-Custom", "value").WithTimeout(5 * time.Second)
//	// base is unaffected; custom has the header and timeout applied.
//
// Clone returns a deep copy. For simple value-typed implementations Clone and the
// With* methods are equivalent in isolation, but Clone is the explicit way to
// communicate "I want an independent copy to diverge from."
//
// Apply applies Param functions in sequence, threading the copy-on-write return
// value through each one. This is the primary way to apply reusable configuration:
//
//	client := base.Apply(
//	    httpc.WithAdditionalHeader[MyClient]("X-Tenant", tenant),
//	    httpc.WithAdditionalTimeout[MyClient](10*time.Second),
//	)
type ServiceClient[C ServiceClient[C]] interface {
	// Clone returns a deep copy of the service client. The copy is independent:
	// subsequent With* calls on either do not affect the other.
	Clone() C

	// Apply applies the given Param functions in sequence, returning a new client
	// with all modifications applied. The original client is not modified.
	Apply(...Param[C]) C

	RequestOverrides[C]
}

// WithAdditionalHeader returns a Param that adds a request header to a ServiceClient.
func WithAdditionalHeader[C ServiceClient[C]](key, value string) Param[C] {
	return func(client C) C { return client.WithHeader(key, value) }
}

// WithAdditionalTimeout returns a Param that sets a per-request timeout on a ServiceClient.
func WithAdditionalTimeout[C ServiceClient[C]](timeout time.Duration) Param[C] {
	return func(client C) C { return client.WithTimeout(timeout) }
}

// WithAdditionalErrorDecoder returns a Param that sets a per-request error decoder on a ServiceClient.
func WithAdditionalErrorDecoder[C ServiceClient[C]](decoder ErrorDecoder) Param[C] {
	return func(client C) C { return client.WithErrorDecoder(decoder) }
}

// WithAdditionalMiddleware returns a Param that appends a per-request middleware to a ServiceClient.
func WithAdditionalMiddleware[C ServiceClient[C]](m Middleware) Param[C] {
	return func(client C) C { return client.WithMiddleware(m) }
}
