package bot

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/app/bot/mocks"
)

func TestGenHelpMsg(t *testing.T) {
	require.Equal(t, "cmd _– description_\n", GenHelpMsg([]string{"cmd"}, "description"))
}

func TestMultiBotHelp(t *testing.T) {
	b1 := &mocks.BotInterface{HelpFunc: func() string {
		return "b1 help"
	}}
	b2 := &mocks.BotInterface{HelpFunc: func() string {
		return "b2 help"
	}}

	// Must return concatenated b1 and b2 without space
	// Line formatting only in GenHelpMsg()
	require.Equal(t, "b1 help\nb2 help\n", MultiBot{b1, b2}.Help())
}

func TestMultiBotReactsOnHelp(t *testing.T) {
	b := &mocks.BotInterface{
		ReactOnFunc: func() []string {
			return []string{"help"}
		},
		HelpFunc: func() string {
			return "help"
		},
	}

	mb := MultiBot{b}
	resp := mb.OnMessage(Message{Text: "help"})

	require.True(t, resp.Send)
	require.Equal(t, "help\n", resp.Text)
}

func TestMultiBotCombinesAllBotResponses(t *testing.T) {
	msg := Message{Text: "cmd"}

	b1 := &mocks.BotInterface{
		ReactOnFunc:   func() []string { return []string{"cmd"} },
		OnMessageFunc: func(m Message) Response { return Response{Send: true, Text: "b1 resp", ReplyTo: 789} },
	}
	b2 := &mocks.BotInterface{
		ReactOnFunc:   func() []string { return []string{"cmd"} },
		OnMessageFunc: func(m Message) Response { return Response{Send: true, Text: "b2 resp", DeleteReplyTo: true} },
	}

	mb := MultiBot{b1, b2}
	resp := mb.OnMessage(msg)
	t.Logf("resp: %+v", resp)

	require.True(t, resp.Send)
	parts := strings.Split(resp.Text, "\n")
	require.Len(t, parts, 2)
	require.Contains(t, parts, "b1 resp")
	require.Contains(t, parts, "b2 resp")
	assert.Equal(t, 789, resp.ReplyTo)
	assert.True(t, resp.DeleteReplyTo)
}
