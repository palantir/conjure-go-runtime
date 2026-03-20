package httpc

import (
	"bytes"
	"crypto/tls"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient/internal"
	"github.com/palantir/conjure-go-runtime/v3/conjure-go-contract/codecs"
	"github.com/palantir/conjure-go-runtime/v3/conjure-go-contract/errors"
	"github.com/palantir/pkg/bytesbuffers"
	"github.com/palantir/pkg/refreshable/v2"
	werror "github.com/palantir/witchcraft-go-error"
)

const (
	defaultDialTimeout           = 10 * time.Second
	defaultHTTPTimeout           = 60 * time.Second
	defaultKeepAlive             = 30 * time.Second
	defaultIdleConnTimeout       = 90 * time.Second
	defaultTLSHandshakeTimeout   = 10 * time.Second
	defaultExpectContinueTimeout = 1 * time.Second
	defaultMaxIdleConns          = 200
	defaultMaxIdleConnsPerHost   = 100
	defaultHTTP2ReadIdleTimeout  = 30 * time.Second
	defaultHTTP2PingTimeout      = 15 * time.Second
	defaultInitialBackoff        = 250 * time.Millisecond
	defaultMaxBackoff            = 2 * time.Second
)

// StandardClientBuilder implements ClientBuilder by directly managing
// transport, dialer, TLS, and service-level configuration.
type StandardClientBuilder struct {
	serviceName     refreshable.Refreshable[string]
	timeout         refreshable.Refreshable[time.Duration]
	dialerParams    refreshable.Refreshable[dialerParams]
	tlsConfig       *tls.Config // escape hatch: replaces all other TLS settings
	transportParams refreshable.Refreshable[transportParams]
	tlsFileParams   refreshable.Refreshable[tlsFileParams] // TLS file/flag settings
	tlsCABytes      refreshable.Refreshable[[][]byte]

	middlewares      []Middleware // outer: applied after built-in middleware
	innerMiddlewares []Middleware // inner: applied before built-in middleware

	disableMetrics      refreshable.Refreshable[bool]
	metricsTagProviders []TagsProvider
	disableRequestSpan  bool
	disableRecovery     bool
	disableTraceHeaders bool

	uris             refreshable.Refreshable[[]string]
	uriScorerBuilder func([]string) internal.URIScoringMiddleware
	allowEmptyURIs   bool

	errorDecoder    ErrorDecoder
	bytesBufferPool bytesbuffers.Pool
	maxAttempts     refreshable.Refreshable[*int]
	initialBackoff  refreshable.Refreshable[time.Duration]
	maxBackoff      refreshable.Refreshable[time.Duration]

	transport        http.RoundTripper // escape hatch: direct transport injection
	caByteSlices     [][]byte          // static CA cert bytes from AddCACertBytes
	clientCertKey    []byte            // client cert key bytes
	clientCertCert   []byte            // client cert bytes
	includeSystemCAs bool
	errs             []error
}

// Compile-time interface check.
var _ ClientBuilder[*StandardClientBuilder] = (*StandardClientBuilder)(nil)

// NewStandardClientBuilder creates a new StandardClientBuilder with sane defaults.
func NewStandardClientBuilder() *StandardClientBuilder {
	return &StandardClientBuilder{
		serviceName: refreshable.New(""),
		timeout:     refreshable.New(defaultHTTPTimeout),
		dialerParams: refreshable.New(dialerParams{
			DialTimeout: defaultDialTimeout,
			KeepAlive:   defaultKeepAlive,
		}),
		transportParams: refreshable.New(transportParams{
			MaxIdleConns:          defaultMaxIdleConns,
			MaxIdleConnsPerHost:   defaultMaxIdleConnsPerHost,
			IdleConnTimeout:       defaultIdleConnTimeout,
			ExpectContinueTimeout: defaultExpectContinueTimeout,
			TLSHandshakeTimeout:   defaultTLSHandshakeTimeout,
			ProxyFromEnvironment:  true,
			HTTP2ReadIdleTimeout:  defaultHTTP2ReadIdleTimeout,
			HTTP2PingTimeout:      defaultHTTP2PingTimeout,
		}),
		tlsFileParams:  refreshable.New(tlsFileParams{}),
		disableMetrics: refreshable.New(false),
		errorDecoder:   defaultRestErrorDecoder{},
		initialBackoff: refreshable.New(defaultInitialBackoff),
		maxBackoff:     refreshable.New(defaultMaxBackoff),
	}
}

// Clone returns a deep copy of the builder.
func (b *StandardClientBuilder) Clone() *StandardClientBuilder {
	var clonedTLSConfig *tls.Config
	if b.tlsConfig != nil {
		clonedTLSConfig = b.tlsConfig.Clone()
	}
	clone := &StandardClientBuilder{
		serviceName:         b.serviceName,
		timeout:             b.timeout,
		dialerParams:        b.dialerParams,
		transportParams:     b.transportParams,
		tlsFileParams:       b.tlsFileParams,
		tlsConfig:           clonedTLSConfig,
		tlsCABytes:          b.tlsCABytes,
		middlewares:         slices.Clone(b.middlewares),
		innerMiddlewares:    slices.Clone(b.innerMiddlewares),
		disableMetrics:      b.disableMetrics,
		metricsTagProviders: slices.Clone(b.metricsTagProviders),
		disableRequestSpan:  b.disableRequestSpan,
		disableRecovery:     b.disableRecovery,
		disableTraceHeaders: b.disableTraceHeaders,
		uris:                b.uris,
		uriScorerBuilder:    b.uriScorerBuilder,
		allowEmptyURIs:      b.allowEmptyURIs,
		errorDecoder:        b.errorDecoder,
		bytesBufferPool:     b.bytesBufferPool,
		maxAttempts:         b.maxAttempts,
		initialBackoff:      b.initialBackoff,
		maxBackoff:          b.maxBackoff,
		transport:           b.transport,
		caByteSlices:        slices.Clone(b.caByteSlices),
		clientCertKey:       bytes.Clone(b.clientCertKey),
		clientCertCert:      bytes.Clone(b.clientCertCert),
		includeSystemCAs:    b.includeSystemCAs,
		errs:                slices.Clone(b.errs),
	}
	return clone
}

// Apply applies the given Param functions to the builder in sequence.
func (b *StandardClientBuilder) Apply(params ...Param[*StandardClientBuilder]) *StandardClientBuilder {
	for _, p := range params {
		p(b)
	}
	return b
}

// defaultRestErrorDecoder handles responses with status code >= 307.
// For JSON responses, it attempts to unmarshal the body as a Conjure error.
// For non-JSON responses or failed unmarshal, it includes the raw body as an
// unsafe parameter. For 3xx responses, it extracts the Location header.
//
// Use StatusCodeFromError(err) to retrieve the code from the error,
// and DisableRestErrors() to disable this decoder on your client.
type defaultRestErrorDecoder struct {
	conjureErrorDecoder errors.ConjureErrorDecoder
}

func (defaultRestErrorDecoder) Handles(resp *http.Response) bool {
	return resp.StatusCode >= http.StatusTemporaryRedirect
}

func (d defaultRestErrorDecoder) DecodeError(resp *http.Response) error {
	safeParams := map[string]interface{}{
		"statusCode": resp.StatusCode,
	}
	unsafeParams := map[string]interface{}{}
	if resp.StatusCode >= http.StatusTemporaryRedirect &&
		resp.StatusCode < http.StatusBadRequest {
		location, err := resp.Location()
		if err == nil {
			unsafeParams["location"] = location.String()
		}
	}
	wSafeParams := werror.SafeParams(safeParams)
	wUnsafeParams := werror.UnsafeParams(unsafeParams)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return werror.Wrap(err, "server returned an error and failed to read body", wSafeParams, wUnsafeParams)
	}
	if len(body) == 0 {
		return werror.Error(resp.Status, wSafeParams, wUnsafeParams)
	}

	// If JSON, try to unmarshal as Conjure error.
	if isJSON := strings.Contains(resp.Header.Get("Content-Type"), codecs.JSON.ContentType()); !isJSON {
		return werror.Error(resp.Status, wSafeParams, wUnsafeParams, werror.UnsafeParam("responseBody", string(body)))
	}
	var conjureErr errors.Error
	var jsonErr error
	if d.conjureErrorDecoder != nil {
		conjureErr, jsonErr = errors.UnmarshalErrorWithDecoder(d.conjureErrorDecoder, body)
	} else {
		conjureErr, jsonErr = errors.UnmarshalError(body)
	}
	if jsonErr != nil {
		return werror.Error(resp.Status, wSafeParams, wUnsafeParams, werror.UnsafeParam("responseBody", string(body)))
	}
	return werror.Wrap(conjureErr, "", wSafeParams, wUnsafeParams)
}

// StatusCodeFromError retrieves the 'statusCode' parameter from the provided error.
// If the error is not a werror or does not have the statusCode param, ok is false.
//
// The default client error decoder sets the statusCode parameter on its returned errors.
// Note that, if a custom error decoder is used, this function will only return a status
// code for the error if the custom decoder sets a 'statusCode' parameter on the error.
func StatusCodeFromError(err error) (statusCode int, ok bool) {
	return internal.StatusCodeFromError(err)
}

// LocationFromError retrieves the 'location' parameter from the provided error.
// If the error is not a werror or does not have the location param, ok is false.
//
// The default client error decoder sets the location parameter on its returned errors
// if the status code is 3xx and a location is set in the response header.
func LocationFromError(err error) (location string, ok bool) {
	return internal.LocationFromError(err)
}
