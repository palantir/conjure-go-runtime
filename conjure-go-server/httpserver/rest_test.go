package httpserver

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateSecretStringEqual(t *testing.T) {
	for _, tc := range []struct {
		name        string
		a           string
		b           string
		expectMatch bool
	}{
		{
			name:        "empty strings match",
			a:           "",
			b:           "",
			expectMatch: true,
		},
		{
			name:        "strings match exactly",
			a:           "secret",
			b:           "secret",
			expectMatch: true,
		},
		{
			name:        "string does not match empty string",
			a:           "secret",
			b:           "",
			expectMatch: false,
		},
		{
			name:        "string does not match other string",
			a:           "invalid",
			b:           "secret",
			expectMatch: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expectMatch, SecretStringEqual(tc.a, tc.b))
		})
	}
}
