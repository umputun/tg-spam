package spamcheck

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResponse_String(t *testing.T) {
	tests := []struct {
		name     string
		input    *Response
		expected string
	}{
		{
			name: "test spam",
			input: &Response{
				Name:    "name1",
				Spam:    true,
				Details: "details",
			},
			expected: "name1: spam, details",
		},
		{
			name: "test ham",
			input: &Response{
				Name:    "name2",
				Spam:    false,
				Details: "details",
			},
			expected: "name2: ham, details",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := tt.input.String()
			assert.Equal(t, tt.expected, output)
		})
	}
}
