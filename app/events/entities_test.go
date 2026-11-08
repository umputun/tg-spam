package events

import (
	"testing"

	tbapi "github.com/OvyFlash/telegram-bot-api"
	"github.com/stretchr/testify/assert"
)

func TestEntityText_Build(t *testing.T) {
	tests := []struct {
		name  string
		build func(e *entityText)
		text  string
		ents  []tbapi.MessageEntity
	}{
		{name: "plain only", build: func(e *entityText) { e.plain("hello"); e.plain(" world") },
			text: "hello world", ents: []tbapi.MessageEntity{}},
		{name: "bold", build: func(e *entityText) { e.plain("a "); e.bold("bc") },
			text: "a bc", ents: []tbapi.MessageEntity{{Type: "bold", Offset: 2, Length: 2}}},
		{name: "italic", build: func(e *entityText) { e.italic("note") },
			text: "note", ents: []tbapi.MessageEntity{{Type: "italic", Offset: 0, Length: 4}}},
		{name: "pre", build: func(e *entityText) { e.plain("x\n\n"); e.pre("https://phish.example") },
			text: "x\n\nhttps://phish.example", ents: []tbapi.MessageEntity{{Type: "pre", Offset: 3, Length: 21}}},
		{name: "link", build: func(e *entityText) { e.link("user", "tg://user?id=1") },
			text: "user", ents: []tbapi.MessageEntity{{Type: "text_link", Offset: 0, Length: 4, URL: "tg://user?id=1"}}},
		{name: "bold span over plain and link", build: func(e *entityText) {
			from := e.mark()
			e.plain("permanently banned ")
			e.link("bob", "tg://user?id=7")
			e.span(from, tbapi.MessageEntity{Type: "bold"})
			e.plain("\n\n")
		}, text: "permanently banned bob\n\n", ents: []tbapi.MessageEntity{
			{Type: "bold", Offset: 0, Length: 22},
			{Type: "text_link", Offset: 19, Length: 3, URL: "tg://user?id=7"},
		}},
		{name: "cyrillic offsets", build: func(e *entityText) { e.plain("привет "); e.bold("мир") },
			text: "привет мир", ents: []tbapi.MessageEntity{{Type: "bold", Offset: 7, Length: 3}}},
		{name: "emoji counts as two units", build: func(e *entityText) { e.plain("⛔︎ 👍 "); e.pre("x🔥y") },
			text: "⛔︎ 👍 x🔥y", ents: []tbapi.MessageEntity{{Type: "pre", Offset: 6, Length: 4}}},
		{name: "braille padding", build: func(e *entityText) { e.plain("⠀⠀"); e.italic("z") },
			text: "⠀⠀z", ents: []tbapi.MessageEntity{{Type: "italic", Offset: 2, Length: 1}}},
		{name: "empty append adds no entity", build: func(e *entityText) { e.plain("a"); e.bold(""); e.pre("") },
			text: "a", ents: []tbapi.MessageEntity{}},
		{name: "empty span adds no entity", build: func(e *entityText) {
			e.plain("a")
			from := e.mark()
			e.span(from, tbapi.MessageEntity{Type: "bold"})
		}, text: "a", ents: []tbapi.MessageEntity{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &entityText{}
			tt.build(e)
			assert.Equal(t, tt.text, e.text())
			assert.Equal(t, tt.ents, e.entities())
		})
	}
}

func TestEntityTextFrom(t *testing.T) {
	text := "banned 👍 bob\n\nspam https://phish.example @scam #tag"
	ents := []tbapi.MessageEntity{
		{Type: "bold", Offset: 0, Length: 13},
		{Type: "text_link", Offset: 10, Length: 3, URL: "tg://user?id=7"},
		{Type: "pre", Offset: 15, Length: 37},
		{Type: "url", Offset: 20, Length: 21},
		{Type: "mention", Offset: 42, Length: 5},
		{Type: "hashtag", Offset: 48, Length: 4},
		{Type: "italic", Offset: 15, Length: 4},
		{Type: "underline", Offset: 15, Length: 4},
		{Type: "strikethrough", Offset: 15, Length: 4},
		{Type: "code", Offset: 15, Length: 4},
		{Type: "text_mention", Offset: 10, Length: 3, User: &tbapi.User{ID: 7}},
	}

	e := entityTextFrom(text, ents)
	e.plain("\n\n")
	e.italic("ban confirmed")

	assert.Equal(t, text+"\n\nban confirmed", e.text())
	assert.Equal(t, []tbapi.MessageEntity{
		{Type: "bold", Offset: 0, Length: 13},
		{Type: "text_link", Offset: 10, Length: 3, URL: "tg://user?id=7"},
		{Type: "text_mention", Offset: 10, Length: 3, User: &tbapi.User{ID: 7}},
		{Type: "pre", Offset: 15, Length: 37},
		{Type: "italic", Offset: 15, Length: 4},
		{Type: "underline", Offset: 15, Length: 4},
		{Type: "strikethrough", Offset: 15, Length: 4},
		{Type: "code", Offset: 15, Length: 4},
		{Type: "italic", Offset: 54, Length: 13},
	}, e.entities())
}
