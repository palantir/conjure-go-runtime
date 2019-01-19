conjure-go-runtime
==================
[![](https://godoc.org/github.com/palantir/conjure-go-runtime?status.svg)](http://godoc.org/github.com/palantir/conjure-go-runtime)

Golang packages for Conjure-flavored RPC. The full specification is at [github.com/palantir/conjure/docs/spec/wire.md](https://github.com/palantir/conjure/blob/master/docs/spec/wire.md).

Packages:

* `conjure-go-contract/codecs`: The `codecs` package defines encode/decode behavior for multiple serialization formats. These are used to manipulate request and response bodies.
* `conjure-go-contract/errors`: The `errors` package defines conjure-formatted error types as described in the [Conjure wire spec](https://github.com/palantir/conjure/blob/master/docs/spec/wire.md#55-conjure-errors).
* `conjure-go-contract/uuid`:  The `uuid` package provides an implementation of the UUID as defined in RFC 4122.
* `conjure-go-client/httpclient`: The `httpclient` package provides a general HTTP client package which can provide a standard library `*http.Client` or a more opinionated `httpclient.Client` implementation. The majority of the documentation below describes this package.

## Client Usage

The `NewHTTPClient(params ...ClientParam)` constructor returns a standard library `*http.Client` configured using Palantir best practices.
It offers customizability via ClientParams passed as arguments; see [`client_params.go`](conjure-go-client/httpclient/client_params.go) for the majority of general-purpose params.
The returned client can be used wherever `http.DefaultClient` can be.

```go
var conf httpclient.ServicesConfig // populate in-code or from yaml

clientConf, err := conf.ClientsConfig("my-service")
if err != nil {
	return err
}
client, err := httpclient.NewHTTPClient(
    httpclient.Config(clientConf),
    httpclient.HTTPTimeout(30 * time.Second),
    httpclient.UserAgent(fmt.Sprintf("%s/%s", appName, appVersion),
    httpclient.NoProxy())

resp, err := client.Post(ctx,
		httpclient.WithRPCMethodName("CreateFoo"),
		httpclient.WithPath(fooEndpoint),
		httpclient.WithJSONRequest(fooInput),
        httpclient.WithJSONResponse(&fooOutput))
```

## Features

### Auth Token Provider

The `httpclient.AuthTokenProvider` ClientParam sets the `Authorization` header on each request using the token from the provider.
Its interface allows an implementation which refreshes and caches the client's credentials over time.

### HTTP2

HTTP2 support is enabled by default in the generated client. If this _must_ be disabled, use the `httpclient.DisableHTTP2()` ClientParam.

### Metrics

The `httpclient.Metrics` ClientParam enables the `client.response` timer metric.
By default, it is tagged with `method`, `family` (of status code), and `service-name`.

### Panic Recovery

The `httpclient.PanicRecovery` ClientParam recovers panics occurring during a round trip and propagates them as errors.

### Round Trip Middleware

Use a `httpclient.Middleware` to read or modify a request before it is sent, or a response before it is returned.
The `transport` package includes a number of ClientParams which use this framework under the hood.

```go
type Middleware interface {
	// RoundTrip mimics the API of http.RoundTripper, but adds a 'next' argument.
	// RoundTrip is responsible for invoking next.RoundTrip(req) and returning the response.
	RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error)
}
```

We use round trip middleware to inject headers, instument metrics, and more.
Custom middleware can be provided using the `httpclient.RoundTripMiddleware` param.

### Docs TODOs
* Retry behavior
* Request body behavior
* Response body behavior
* Error handling
* REST error creation

## Client Configuration

`config.go` includes a yaml-tagged ServicesConfig struct for use in application config objects.
The `httpclient.Config` ClientParam applies a configuration to a client builder.

Example configuration, inspired by [http-remoting-api](https://github.com/palantir/http-remoting-api):
```yml
clients:
  max-num-retries: 3
  security:
    ca-files:
    - var/security/ca.pem
    cert-file: var/security/cert.pem
    key-file:  var/security/key.pem
  services:
    api-1:
      uris:
      - https://api-1.example.com/api
      security:
        # Use custom keypair with api-1.
        cert-file: var/security/api-1/cert.pem
        key-file:  var/security/api-1/key.pem
        # Use default system CA certs for external connections
        ca-files: []
    api-2:
      uris:
      - https://api-2a.example.com/api
      - https://api-2b.example.com/api
      - https://api-2c.example.com/api
```

License
-------
This project is made available under the [Apache 2.0 License](http://www.apache.org/licenses/LICENSE-2.0).
