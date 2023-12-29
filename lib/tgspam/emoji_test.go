package tgspam

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

//nolint:stylecheck // it has unicode symbols purposely
func Test_countEmoji(t *testing.T) {
	tests := []struct {
		name  string
		input string
		count int
	}{
		{"NoEmoji", "Hello, world!", 0},
		{"OneEmoji", "Hi there 👋", 1},
		{"TwoEmojis", "Good morning 🌞🌻", 2},
		{"Mixed", "👨‍👩‍👧‍👦 Family emoji", 1},
		{"EmojiSequences", "🏳️‍🌈 Rainbow flag", 1},
		{"TextAfterEmoji", "😊 Have a nice day!", 1},
		{"OnlyEmojis", "😁🐶🍕", 3},
		{"WithCyrillic", "Привет 🌞 🍕 мир! 👋", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.count, countEmoji(tt.input))
		})
	}
}

//nolint:stylecheck // it has unicode symbols purposely
func Test_cleanEmoji(t *testing.T) {
	tests := []struct {
		name  string
		input string
		clean string
	}{
		{"NoEmoji", "Hello, world!", "Hello, world!"},
		{"OneEmoji", "Hi there 👋", "Hi there "},
		{"TwoEmojis", "Good morning 🌞🌻", "Good morning "},
		{"Mixed", "👨‍👩‍👧‍👦 Family emoji", " Family emoji"},
		{"EmojiSequences", "🏳️‍🌈 Rainbow flag", " Rainbow flag"},
		{"TextAfterEmoji", "😊 Have a nice day!", " Have a nice day!"},
		{"OnlyEmojis", "😁🐶🍕", ""},
		{"WithCyrillic", "Привет 🌞 🍕 мир! 👋", "Привет   мир! "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.clean, cleanEmoji(tt.input))
		})
	}
}
