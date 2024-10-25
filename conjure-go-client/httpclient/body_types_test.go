package httpclient

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContentLengthInMemory(t *testing.T) {
	t.Run("bytes.Buffer", func(t *testing.T) {
		assert.EqualValues(t, 5, contentLengthInMemory(bytes.NewBuffer([]byte("hello"))))
	})
	t.Run("bytes.Reader", func(t *testing.T) {
		assert.EqualValues(t, 5, contentLengthInMemory(bytes.NewReader([]byte("hello"))))
	})
	t.Run("strings.Reader", func(t *testing.T) {
		assert.EqualValues(t, 5, contentLengthInMemory(strings.NewReader("hello")))
	})
}
