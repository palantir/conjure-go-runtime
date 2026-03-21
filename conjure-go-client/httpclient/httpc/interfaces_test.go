package httpc

// Compile-time interface satisfaction checks.

// Endpoint satisfies RequestOverrides.
var _ RequestOverrides[Endpoint[string, string]] = Endpoint[string, string]{}

// Overrides satisfies RequestOverrides.
var _ RequestOverrides[Overrides] = Overrides{}

// MiddlewareFunc satisfies Middleware.
var _ Middleware = MiddlewareFunc(nil)

// Tags satisfies TagsProvider.
var _ TagsProvider = Tags(nil)

// BodyEncoderFunc satisfies BodyEncoder.
var _ BodyEncoder[string] = BodyEncoderFunc[string]{}

// BodyDecoderFunc satisfies BodyDecoder.
var _ BodyDecoder[string] = BodyDecoderFunc[string]{}

// fluentClient satisfies Client.
var _ Client = (*fluentClient[*Builder])(nil)

// fluentClient satisfies ConfigurableClient.
var _ ConfigurableClient[*Builder] = (*fluentClient[*Builder])(nil)
