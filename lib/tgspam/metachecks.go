package tgspam

import (
	"fmt"
	"strings"

	"github.com/umputun/tg-spam/lib/spamcheck"
)

// MetaCheck represents a function type that takes a `spamcheck.Request` as input and returns a boolean value and a `spamcheck.Response`.
// The boolean value indicates whether the check. It checks the message's meta.
type MetaCheck func(req spamcheck.Request) spamcheck.Response

// LinksCheck is a function that returns a MetaCheck function that checks the number of links in the message.
// It uses custom meta-info if it is provided, otherwise it counts the number of links in the message.
func LinksCheck(limit int) MetaCheck {
	return func(req spamcheck.Request) spamcheck.Response {
		links := req.Meta.Links
		if links == 0 {
			links = strings.Count(req.Msg, "http://") + strings.Count(req.Msg, "https://")
		}
		if links > limit {
			return spamcheck.Response{
				Name:    "links",
				Spam:    true,
				Details: fmt.Sprintf("too many links %d/%d", links, limit),
			}
		}
		return spamcheck.Response{Spam: false, Name: "links", Details: fmt.Sprintf("links %d/%d", links, limit)}
	}
}

// ImagesCheck is a function that returns a MetaCheck function.
// It checks if the number of images in the message is greater than zero and the message is empty (i.e. it contains only images).
func ImagesCheck() MetaCheck {
	return func(req spamcheck.Request) spamcheck.Response {
		if req.Meta.Images > 0 && req.Msg == "" {
			return spamcheck.Response{
				Name:    "images",
				Spam:    true,
				Details: "images without text",
			}
		}
		return spamcheck.Response{Spam: false, Name: "images", Details: "no images without text"}
	}
}
