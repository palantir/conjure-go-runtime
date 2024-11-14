package errors

type ConjureErrorDecoder interface {
	DecodeConjureError(name string, body []byte) (Error, error)
}
