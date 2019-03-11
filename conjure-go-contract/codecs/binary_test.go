package codecs_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/palantir/conjure-go-runtime/conjure-go-contract/codecs"
)

func TestCodecBinary(t *testing.T) {
	data := []byte(`1234567890`)

	t.Run("Unmarshal", func(t *testing.T) {
		buf := bytes.Buffer{}
		err := codecs.Binary.Unmarshal(data, &buf)
		require.NoError(t, err)
		require.Equal(t, data, buf.Bytes())
	})
	t.Run("Marshal", func(t *testing.T) {
		out, err := codecs.Binary.Marshal(bytes.NewReader(data))
		require.NoError(t, err)
		require.Equal(t, data, out)
	})
}
