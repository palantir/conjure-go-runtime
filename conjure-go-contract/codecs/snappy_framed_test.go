package codecs

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSnappyFramingCompression(t *testing.T) {
	input := "hello world"
	snappyEncoder := SnappyFraming(Plain)

	var buf bytes.Buffer
	err := snappyEncoder.Encode(&buf, input)
	require.NoError(t, err)

	var actual string
	err = snappyEncoder.Decode(strings.NewReader(buf.String()), &actual)
	require.NoError(t, err)

	require.Equal(t, input, actual)
}
