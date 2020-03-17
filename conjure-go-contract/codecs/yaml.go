package codecs

import (
	"fmt"
	"io"

	"gopkg.in/yaml.v2"
)

const (
	contentTypeYAML = "application/x-yaml"
)

var YAML Codec = codecYAML{}

type codecYAML struct{}

func (codecYAML) Accept() string {
	return contentTypeYAML
}

func (codecYAML) Decode(r io.Reader, v interface{}) error {
	if err := yaml.NewDecoder(r).Decode(v); err != nil {
		return fmt.Errorf("failed to decode YAML-encoded value: %s", err.Error())
	}
	return nil
}

func (c codecYAML) Unmarshal(data []byte, v interface{}) error {
	return yaml.Unmarshal(data, v)
}

func (codecYAML) ContentType() string {
	return contentTypeYAML
}

func (codecYAML) Encode(w io.Writer, v interface{}) error {
	if err := yaml.NewEncoder(w).Encode(v); err != nil {
		return fmt.Errorf("failed to YAML-encode value: %s", err.Error())
	}
	return nil
}

func (c codecYAML) Marshal(v interface{}) ([]byte, error) {
	return yaml.Marshal(v)
}
