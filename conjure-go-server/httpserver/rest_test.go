package httpserver

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateSecretStringEqual(t *testing.T) {
	for _, tc := range []struct {
		name           string
		input          string
		expectedSecret string
		expectMatch    bool
	}{
		{
			name:           "input and expected match with empty strings",
			input:          "",
			expectedSecret: "",
			expectMatch:    true,
		},
		{
			name:           "input and expected match with a secret",
			input:          "secret",
			expectedSecret: "secret",
			expectMatch:    true,
		},
		{
			name:           "input does not match secret with empty string",
			input:          "secret",
			expectedSecret: "",
			expectMatch:    false,
		},
		{
			name:           "input does not match secret",
			input:          "invalid",
			expectedSecret: "secret",
			expectMatch:    false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expectMatch, SecretStringEqual(tc.expectedSecret, tc.input))
		})
	}
}
