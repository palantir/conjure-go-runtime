package httpc

import "time"

// DialerBuilder is an F-bounded interface for configuring TCP dialer settings.
//
// Like all builders in this package, DialerBuilder is mutable: setter methods modify
// the receiver and return it for chaining. Use Clone to fork an independent copy.
//
// Generic functions can accept any DialerBuilder and return the same concrete type:
//
//	func ConfigureDialer[Self DialerBuilder[Self]](b Self) Self {
//	    return b.SetDialTimeout(5 * time.Second).SetKeepAlive(15 * time.Second)
//	}
type DialerBuilder[B DialerBuilder[B]] interface {
	// Clone returns a deep copy of the builder. The copy is fully independent:
	// mutations to either the original or the clone do not affect the other.
	Clone() B

	// Apply applies the given Param functions to the builder in sequence.
	// Each Param may call setter methods to configure the builder.
	// Because builders are mutable, this modifies the receiver in place.
	Apply(...Param[B]) B

	// SetDialTimeout sets the maximum duration for establishing a TCP connection.
	// Default: 10s.
	SetDialTimeout(time.Duration) B

	// SetKeepAlive sets the interval between TCP keep-alive probes.
	// Default: 30s.
	SetKeepAlive(time.Duration) B

	// SetSocksProxyURL sets a SOCKS5 proxy URL for TCP connections.
	// Pass "" to clear. Only socks5:// URLs are supported.
	// HTTP/HTTPS proxy configuration is on TransportBuilder.
	SetSocksProxyURL(string) B
}
