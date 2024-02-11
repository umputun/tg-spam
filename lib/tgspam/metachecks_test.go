package tgspam

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/umputun/tg-spam/lib/spamcheck"
)

func TestLinksCheck(t *testing.T) {
	tests := []struct {
		name     string
		req      spamcheck.Request
		limit    int
		expected spamcheck.Response
	}{
		{
			name: "No links",
			req: spamcheck.Request{
				Msg: "This is a message without links.",
			},
			limit:    2,
			expected: spamcheck.Response{Name: "links", Spam: false, Details: "links 0/2"},
		},
		{
			name: "Below limit with meta",
			req: spamcheck.Request{
				Msg: "Check out this link: http://example.com",
				Meta: spamcheck.MetaData{
					Links: 1,
				},
			},
			limit:    2,
			expected: spamcheck.Response{Name: "links", Spam: false, Details: "links 1/2"},
		},
		{
			name: "Above limit with meta",
			req: spamcheck.Request{
				Msg: "Too many links here: http://example.com and https://example.org",
				Meta: spamcheck.MetaData{
					Links: 3,
				},
			},
			limit: 2,
			expected: spamcheck.Response{
				Name:    "links",
				Spam:    true,
				Details: "too many links 3/2",
			},
		},
		{
			name: "Above limit by counting in message",
			req: spamcheck.Request{
				Msg: "Too many links here: http://example.com and https://example.org",
			},
			limit: 1,
			expected: spamcheck.Response{
				Name:    "links",
				Spam:    true,
				Details: "too many links 2/1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check := LinksCheck(tt.limit)
			assert.Equal(t, tt.expected, check(tt.req))
		})
	}
}

func TestLinkOnlyCheck(t *testing.T) {
	tests := []struct {
		name     string
		req      spamcheck.Request
		expected spamcheck.Response
	}{
		{
			name: "with no links",
			req: spamcheck.Request{
				Msg: "This is a message without links.",
			},
			expected: spamcheck.Response{Name: "link-only", Spam: false, Details: "message contains text"},
		},
		{
			name: "with only links",
			req: spamcheck.Request{
				Msg: "http://example.com https://example.org",
			},
			expected: spamcheck.Response{Name: "link-only", Spam: true, Details: "message contains links only"},
		},
		{
			name: "with a single link, no text",
			req: spamcheck.Request{
				Msg: " https://example.org ",
			},
			expected: spamcheck.Response{Name: "link-only", Spam: true, Details: "message contains links only"},
		},
		{
			name: "with text and links",
			req: spamcheck.Request{
				Msg: "Check out this link: http://example.com",
			},
			expected: spamcheck.Response{Name: "link-only", Spam: false, Details: "message contains text"},
		},
		{
			name: "Empty message",
			req: spamcheck.Request{
				Msg: "",
			},
			expected: spamcheck.Response{Name: "link-only", Spam: false, Details: "empty message"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check := LinkOnlyCheck()
			assert.Equal(t, tt.expected, check(tt.req))
		})
	}
}

func TestImagesCheck(t *testing.T) {
	tests := []struct {
		name     string
		req      spamcheck.Request
		expected spamcheck.Response
	}{
		{
			name: "No images and text",
			req: spamcheck.Request{
				Msg: "This is a message with text.",
				Meta: spamcheck.MetaData{
					Images: 0,
				},
			},
			expected: spamcheck.Response{Name: "images", Spam: false, Details: "no images without text"},
		},
		{
			name: "Images with text",
			req: spamcheck.Request{
				Msg: "This is a message with text and an image.",
				Meta: spamcheck.MetaData{
					Images: 1,
				},
			},
			expected: spamcheck.Response{Name: "images", Spam: false, Details: "no images without text"},
		},
		{
			name: "Images without text",
			req: spamcheck.Request{
				Msg: "",
				Meta: spamcheck.MetaData{
					Images: 1,
				},
			},
			expected: spamcheck.Response{
				Name:    "images",
				Spam:    true,
				Details: "images without text",
			},
		},
		{
			name: "Multiple images without text",
			req: spamcheck.Request{
				Msg: "",
				Meta: spamcheck.MetaData{
					Images: 3,
				},
			},
			expected: spamcheck.Response{
				Name:    "images",
				Spam:    true,
				Details: "images without text",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check := ImagesCheck()
			assert.Equal(t, tt.expected, check(tt.req))
		})
	}
}
