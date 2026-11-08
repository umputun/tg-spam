package events

import (
	"sort"
	"strings"
	"unicode/utf16"

	tbapi "github.com/OvyFlash/telegram-bot-api"
)

// offsets and lengths use UTF-16 code units, as required by the Bot API
type entityText struct {
	buf  strings.Builder
	size int
	ents []tbapi.MessageEntity
}

// url, mention and other auto-detected types are dropped: telegram detects them again on send
func entityTextFrom(text string, ents []tbapi.MessageEntity) *entityText {
	e := &entityText{}
	e.plain(text)
	for _, ent := range ents {
		switch ent.Type {
		case "bold", "italic", "underline", "strikethrough", "code", "pre", "text_link", "text_mention":
			e.ents = append(e.ents, ent)
		}
	}
	return e
}

func (e *entityText) plain(s string) {
	e.buf.WriteString(s)
	e.size += e.units(s)
}

func (e *entityText) add(s string, ent tbapi.MessageEntity) {
	if s == "" {
		return
	}
	ent.Offset, ent.Length = e.size, e.units(s)
	e.plain(s)
	e.ents = append(e.ents, ent)
}

func (e *entityText) bold(s string) { e.add(s, tbapi.MessageEntity{Type: "bold"}) }

func (e *entityText) italic(s string) { e.add(s, tbapi.MessageEntity{Type: "italic"}) }

func (e *entityText) pre(s string) { e.add(s, tbapi.MessageEntity{Type: "pre"}) }

func (e *entityText) link(label, url string) {
	e.add(label, tbapi.MessageEntity{Type: "text_link", URL: url})
}

// untrusted adds text taken from a spam message or its check details; with noLinks it goes in pre,
// which telegram clients do not linkify
func (e *entityText) untrusted(s string, noLinks bool) {
	if noLinks {
		e.pre(s)
		return
	}
	e.plain(s)
}

func (e *entityText) mark() int { return e.size }

func (e *entityText) span(from int, ent tbapi.MessageEntity) {
	if e.size == from {
		return
	}
	ent.Offset, ent.Length = from, e.size-from
	e.ents = append(e.ents, ent)
}

func (e *entityText) units(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

func (e *entityText) text() string { return e.buf.String() }

func (e *entityText) entities() []tbapi.MessageEntity {
	res := make([]tbapi.MessageEntity, len(e.ents))
	copy(res, e.ents)
	sort.SliceStable(res, func(i, j int) bool {
		if res[i].Offset != res[j].Offset {
			return res[i].Offset < res[j].Offset
		}
		return res[i].Length > res[j].Length
	})
	return res
}
