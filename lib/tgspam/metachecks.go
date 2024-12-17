package tgspam

import (
	"fmt"
	"regexp"
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

var linkRe = regexp.MustCompile(`https?://\S+`)

// LinkOnlyCheck is a function that returns a MetaCheck function that checks if the req.Msg contains only links.
func LinkOnlyCheck() MetaCheck {
	return func(req spamcheck.Request) spamcheck.Response {
		if strings.TrimSpace(req.Msg) == "" {
			return spamcheck.Response{
				Name:    "link-only",
				Spam:    false,
				Details: "empty message",
			}
		}
		msgWithoutLinks := linkRe.ReplaceAllString(req.Msg, "")
		msgWithoutLinks = strings.TrimSpace(msgWithoutLinks)

		if msgWithoutLinks == "" {
			return spamcheck.Response{
				Name:    "link-only",
				Spam:    true,
				Details: "message contains links only",
			}
		}
		return spamcheck.Response{Spam: false, Name: "link-only", Details: "message contains text"}
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

// VideosCheck is a function that returns a MetaCheck function.
// It checks if the message has a video or video note and the message is empty (i.e. it contains only videos).
func VideosCheck() MetaCheck {
	return func(req spamcheck.Request) spamcheck.Response {
		if req.Meta.HasVideo && req.Msg == "" {
			return spamcheck.Response{
				Name:    "videos",
				Spam:    true,
				Details: "videos without text",
			}
		}
		return spamcheck.Response{Spam: false, Name: "videos", Details: "no videos without text"}
	}
}

// ForwardedCheck is a function that returns a MetaCheck function.
// It checks if the message is a forwarded message.
func ForwardedCheck() MetaCheck {
	return func(req spamcheck.Request) spamcheck.Response {
		if req.Meta.HasForward {
			return spamcheck.Response{
				Name:    "forward",
				Spam:    true,
				Details: "forwarded message",
			}
		}
		return spamcheck.Response{Spam: false, Name: "forward", Details: "not forwarded message"}
	}

}
