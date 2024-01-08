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

func TestRequestString(t *testing.T) {
	tests := []struct {
		name     string
		request  Request
		expected string
	}{
		{
			name:     "Normal message",
			request:  Request{"Hello, world!", "123", "Alice", MetaData{2, 1}},
			expected: `msg:"Hello, world!", user:"Alice", id:123, images:2, links:1`,
		},
		{
			name:     "Spam message",
			request:  Request{"Spam message", "456", "Bob", MetaData{0, 3}},
			expected: `msg:"Spam message", user:"Bob", id:456, images:0, links:3`,
		},
		{
			name:     "Empty fields",
			request:  Request{"", "", "", MetaData{0, 0}},
			expected: `msg:"", user:"", id:, images:0, links:0`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.request.String())
		})
	}
}
