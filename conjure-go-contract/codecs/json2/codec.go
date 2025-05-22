package json2

import (
	"bytes"
	stdjson "encoding/json"
	"io"

	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	werror "github.com/palantir/witchcraft-go-error"
)

// ClientCodec implements conjure-go-runtime's Codec interface.
// It ignores unknown struct members.
var ClientCodec codecClient

// ServerCodec implements conjure-go-runtime's Codec interface.
// It rejects unknown struct members.
var ServerCodec codecServer

var (
	marshalOptions = json.JoinOptions(
		json.Deterministic(true),
		json.WithMarshalers(json.JoinMarshalers(
			// marshals a json.Number as-is, since this type is not recognized by the json v2 encoder and gets quoted as a string.
			json.MarshalFunc(func(number stdjson.Number) ([]byte, error) { return []byte(number), nil }),
			// marshals a json.RawMessage as-is, since this type is not recognized by the json v2 encoder and gets quoted as bytes.
			json.MarshalFunc(func(message stdjson.RawMessage) ([]byte, error) { return message, nil }),
		)))
	unmarshalOptions = json.JoinOptions(
		json.WithUnmarshalers(json.JoinUnmarshalers(
			// unmarshals a json.Number as-is, since this type is not recognized by the json v2 encoder and gets quoted as a string.
			json.UnmarshalFunc[*stdjson.Number](func(data []byte, number *stdjson.Number) error { return assign(number, stdjson.Number(data)) }),
			// unmarshals a json.RawMessage as-is, since this type is not recognized by the json v2 encoder and gets quoted as bytes.
			json.UnmarshalFunc[*stdjson.RawMessage](func(data []byte, message *stdjson.RawMessage) error { return assign(message, data) }),
		)))
	// serverUnmarshalOptions is a copy of unmarshalOptions with RejectUnknownMembers(true)
	serverUnmarshalOptions = json.JoinOptions(unmarshalOptions, json.RejectUnknownMembers(true))
)

type codecClient struct{ codecBase }

func (codecClient) Decode(r io.Reader, v any) error {
	if unmarshaler, ok := v.(json.UnmarshalerFrom); ok {
		if err := unmarshaler.UnmarshalJSONFrom(jsontext.NewDecoder(r, unmarshalOptions)); err != nil {
			return werror.Convert(err)
		}
	} else {
		if err := json.UnmarshalRead(r, *&v, unmarshalOptions); err != nil {
			return werror.Convert(err)
		}
	}
	return nil
}

func (codecClient) Unmarshal(data []byte, v any) error {
	if unmarshaler, ok := v.(json.UnmarshalerFrom); ok {
		if err := unmarshaler.UnmarshalJSONFrom(jsontext.NewDecoder(bytes.NewBuffer(data), unmarshalOptions)); err != nil {
			return werror.Convert(err)
		}
	} else {
		if err := json.Unmarshal(data, *&v, unmarshalOptions); err != nil {
			return werror.Convert(err)
		}
	}
	return nil
}

type codecServer struct{ codecBase }

func (codecServer) Decode(r io.Reader, v any) error {
	if unmarshaler, ok := v.(json.UnmarshalerFrom); ok {
		if err := unmarshaler.UnmarshalJSONFrom(jsontext.NewDecoder(r, serverUnmarshalOptions)); err != nil {
			return werror.Convert(err)
		}
	} else {
		if err := json.UnmarshalRead(r, *&v, serverUnmarshalOptions); err != nil {
			return werror.Convert(err)
		}
	}
	return nil
}

func (codecServer) Unmarshal(data []byte, v any) error {
	if unmarshaler, ok := v.(json.UnmarshalerFrom); ok {
		if err := unmarshaler.UnmarshalJSONFrom(jsontext.NewDecoder(bytes.NewBuffer(data), serverUnmarshalOptions)); err != nil {
			return werror.Convert(err)
		}
	} else {
		if err := json.Unmarshal(data, *&v, serverUnmarshalOptions); err != nil {
			return werror.Convert(err)
		}
	}
	return nil
}

type codecBase struct{}

func (codecBase) Accept() string {
	return "application/json"
}

func (codecBase) ContentType() string {
	return "application/json"
}

func (codecBase) Encode(w io.Writer, v any) error {
	if marshaler, ok := v.(json.MarshalerTo); ok {
		if err := marshaler.MarshalJSONTo(jsontext.NewEncoder(w, marshalOptions)); err != nil {
			return werror.Convert(err)
		}
	} else {
		if err := json.MarshalWrite(w, v, marshalOptions); err != nil {
			return werror.Convert(err)
		}
	}
	return nil
}

func (codecBase) Marshal(v any) ([]byte, error) {
	if marshaler, ok := v.(json.MarshalerTo); ok {
		buf := bytes.NewBuffer(nil)
		if err := marshaler.MarshalJSONTo(jsontext.NewEncoder(buf, marshalOptions)); err != nil {
			return nil, werror.Convert(err)
		}
		return buf.Bytes(), nil
	}
	data, err := json.Marshal(v, marshalOptions)
	if err != nil {
		return nil, werror.Convert(err)
	}
	return data, nil
}

func assign[T any](receiver *T, data T) error {
	*receiver = data
	return nil
}
