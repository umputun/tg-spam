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

// AudioCheck is a function that returns a MetaCheck function.
// It checks if the message has an audio and the message is empty (i.e. it contains only audio).
func AudioCheck() MetaCheck {
	return func(req spamcheck.Request) spamcheck.Response {
		if req.Meta.HasAudio && req.Msg == "" {
			return spamcheck.Response{
				Name:    "audio",
				Spam:    true,
				Details: "audio without text",
			}
		}
		return spamcheck.Response{Spam: false, Name: "audio", Details: "no audio without text"}
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
		return spamcheck.Response{
			Name:    "forward",
			Spam:    false,
			Details: "not a forwarded message",
		}
	}
}

// KeyboardCheck is a function that returns a MetaCheck function.
// It checks if the message has a keyboard (buttons).
func KeyboardCheck() MetaCheck {
	return func(req spamcheck.Request) spamcheck.Response {
		if req.Meta.HasKeyboard {
			return spamcheck.Response{
				Name:    "keyboard",
				Spam:    true,
				Details: "message with keyboard",
			}
		}
		return spamcheck.Response{
			Name:    "keyboard",
			Spam:    false,
			Details: "no keyboard",
		}
	}
}

// MentionsCheck is a function that returns a MetaCheck function.
// It checks if the number of mentions in the message exceeds the specified limit.
// If limit is negative, the check is disabled.
func MentionsCheck(limit int) MetaCheck {
	return func(req spamcheck.Request) spamcheck.Response {
		if limit < 0 {
			return spamcheck.Response{
				Name:    "mentions",
				Spam:    false,
				Details: "check disabled",
			}
		}
		if req.Meta.Mentions > limit {
			return spamcheck.Response{
				Name:    "mentions",
				Spam:    true,
				Details: fmt.Sprintf("too many mentions %d/%d", req.Meta.Mentions, limit),
			}
		}
		return spamcheck.Response{
			Name:    "mentions",
			Spam:    false,
			Details: fmt.Sprintf("mentions %d/%d", req.Meta.Mentions, limit),
		}
	}
}

// UsernameSymbolsCheck is a function that returns a MetaCheck function.
// It checks if the username contains any of the prohibited symbols.
// If symbols is empty, the check is disabled.
func UsernameSymbolsCheck(symbols string) MetaCheck {
	return func(req spamcheck.Request) spamcheck.Response {
		if symbols == "" {
			return spamcheck.Response{
				Name:    "username-symbols",
				Spam:    false,
				Details: "check disabled",
			}
		}

		if req.UserName == "" {
			return spamcheck.Response{
				Name:    "username-symbols",
				Spam:    false,
				Details: "no username",
			}
		}

		for _, symbol := range symbols {
			if strings.ContainsRune(req.UserName, symbol) {
				return spamcheck.Response{
					Name:    "username-symbols",
					Spam:    true,
					Details: fmt.Sprintf("username contains prohibited symbol '%c'", symbol),
				}
			}
		}

		return spamcheck.Response{
			Name:    "username-symbols",
			Spam:    false,
			Details: "no prohibited symbols in username",
		}
	}
}
