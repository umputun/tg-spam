package gomoji

import (
	"errors"
	"strings"
	"unicode"

	"github.com/rivo/uniseg"
)

// errors
var (
	ErrStrNotEmoji = errors.New("the string is not emoji")
)

// Emoji is an entity that represents comprehensive emoji info.
type Emoji struct {
	Slug        string `json:"slug"`
	Character   string `json:"character"`
	UnicodeName string `json:"unicode_name"`
	CodePoint   string `json:"code_point"`
	Group       string `json:"group"`
	SubGroup    string `json:"sub_group"`
}

// ContainsEmoji checks whether given string contains emoji or not. It uses local emoji list as provider.
func ContainsEmoji(s string) bool {
	for _, r := range s {
		if _, ok := emojiMap[string(r)]; ok {
			return true
		}
	}

	gr := uniseg.NewGraphemes(s)
	for gr.Next() {
		if _, ok := emojiMap[gr.Str()]; ok {
			return true
		}
	}

	return false
}

// AllEmojis gets all emojis from provider.
func AllEmojis() []Emoji {
	return emojiMapToSlice(emojiMap)
}

// RemoveEmojis removes all emojis from the s string and returns a new string.
func RemoveEmojis(s string) string {
	return ReplaceEmojisWithFunc(s, nil)
}

// ReplaceEmojisWith replaces all emojis from the s string with the specified rune and returns a new string.
func ReplaceEmojisWith(s string, c rune) string {
	replacerStr := string(c)
	return ReplaceEmojisWithFunc(s, func(_ Emoji) string {
		return replacerStr
	})
}

// ReplaceEmojisWithSlug replaces all emojis from the s string with the emoji's slug and returns a new string.
func ReplaceEmojisWithSlug(s string) string {
	return ReplaceEmojisWithFunc(s, func(em Emoji) string {
		return em.Slug
	})
}

type replacerFn func(e Emoji) string

// ReplaceEmojisWithFunc replaces all emojis from the s string with the result of the replacerFn function and returns a new string.
func ReplaceEmojisWithFunc(s string, replacer replacerFn) string {
	var buf strings.Builder

	gr := uniseg.NewGraphemes(s)
	for gr.Next() {
		cluster := gr.Str()

		lookup := strings.Map(func(r rune) rune {
			if unicode.In(r, unicode.Variation_Selector) {
				return -1
			}
			return r
		}, cluster)

		if em, ok := emojiMap[lookup]; ok {
			if replacer != nil {
				buf.WriteString(replacer(em))
			}
			continue
		}

		buf.WriteString(cluster)
	}

	return strings.Map(func(r rune) rune {
		if unicode.In(r, unicode.Variation_Selector) {
			return -1
		}
		return r
	}, buf.String())
}

// GetInfo returns a gomoji.Emoji model representation of provided emoji.
// If the emoji was not found, it returns the gomoji.ErrStrNotEmoji error
func GetInfo(emoji string) (Emoji, error) {
	em, ok := emojiMap[emoji]
	if !ok {
		return Emoji{}, ErrStrNotEmoji
	}

	return em, nil
}

// CollectAll finds all emojis in given string. Unlike FindAll, this does not
// distinct repeating occurrences of emoji. If there are no emojis it returns a nil-slice.
func CollectAll(s string) []Emoji {
	var emojis []Emoji

	gr := uniseg.NewGraphemes(s)
	for gr.Next() {
		if em, ok := emojiMap[gr.Str()]; ok {
			emojis = append(emojis, em)
			continue
		}

		start, end := gr.Positions()
		for _, r := range s[start:end] {
			if em, ok := emojiMap[string(r)]; ok {
				emojis = append(emojis, em)
			}
		}
	}

	return emojis
}

// FindAll finds all emojis in given string. If there are no emojis it returns a nil-slice.
func FindAll(s string) []Emoji {
	emojis := make(map[string]Emoji)
	gr := uniseg.NewGraphemes(s)

	for gr.Next() {
		cluster := gr.Str()
		if em, ok := emojiMap[cluster]; ok {
			emojis[cluster] = em
			continue
		}

		// Sub-cluster analysis for partial matches
		for i := len(cluster); i > 0; i-- {
			sub := cluster[:i]
			if em, ok := emojiMap[sub]; ok {
				emojis[sub] = em
				break
			}
		}
	}

	return emojiMapToSlice(emojis)
}

func emojiMapToSlice(em map[string]Emoji) []Emoji {
	var emojis []Emoji
	for _, emoji := range em {
		emojis = append(emojis, emoji)
	}

	return emojis
}
