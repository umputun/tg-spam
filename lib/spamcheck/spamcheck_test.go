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
			name: "Normal message",
			request: Request{
				Msg: "Hello, world!", UserID: "123", UserName: "Alice", Meta: MetaData{
					Images: 2, Links: 1, Mentions: 0, HasVideo: false, HasAudio: false, HasForward: false, HasKeyboard: false,
				}, CheckOnly: false},
			expected: `msg:"Hello, world!", user:"Alice", id:123, images:2, links:1, mentions:0, has_video:false, has_audio:false, has_forward:false, has_keyboard:false`,
		},
		{
			name: "Spam message",
			request: Request{
				Msg: "Spam message", UserID: "456", UserName: "Bob", Meta: MetaData{
					Images: 0, Links: 3, Mentions: 2, HasVideo: true, HasAudio: false, HasForward: false, HasKeyboard: false,
				}, CheckOnly: true},
			expected: `msg:"Spam message", user:"Bob", id:456, images:0, links:3, mentions:2, has_video:true, has_audio:false, has_forward:false, has_keyboard:false`,
		},
		{
			name:     "Empty fields",
			request:  Request{Msg: "", UserID: "", UserName: "", Meta: MetaData{}, CheckOnly: false},
			expected: `msg:"", user:"", id:, images:0, links:0, mentions:0, has_video:false, has_audio:false, has_forward:false, has_keyboard:false`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.request.String())
		})
	}
}

func TestChecksToString(t *testing.T) {
	tests := []struct {
		name     string
		checks   []Response
		expected string
	}{
		{
			name: "single check",
			checks: []Response{
				{Name: "check1", Spam: true, Details: "details1"},
			},
			expected: "[{check1: spam, details1}] ",
		},
		{
			name: "multiple checks",
			checks: []Response{
				{Name: "check1", Spam: true, Details: "details1"},
				{Name: "check2", Spam: false, Details: "details2"},
			},
			expected: "[{check1: spam, details1}, {check2: ham, details2}] ",
		},
		{
			name:     "no checks",
			checks:   []Response{},
			expected: "[] ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := ChecksToString(tt.checks)
			assert.Equal(t, tt.expected, output)
		})
	}
}
