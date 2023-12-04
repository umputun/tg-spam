package bot

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

//go:generate moq --out mocks/http_client.go --pkg mocks --skip-ensure . HTTPClient:HTTPClient
//go:generate moq --out mocks/super_user.go --pkg mocks --skip-ensure . SuperUser:SuperUser

// Response describes bot's reaction on particular message
type Response struct {
	Text          string
	Send          bool          // status
	BanInterval   time.Duration // bots banning user set the interval
	User          User          // user to ban
	ChannelID     int64         // channel to ban, if set then User and BanInterval are ignored
	ReplyTo       int           // message to reply to, if 0 then no reply but common message
	ParseMode     string        // parse mode for message in Telegram (we use Markdown by default)
	DeleteReplyTo bool          // delete message what bot replays to
}

// HTTPClient wrap http.Client to allow mocking
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// SuperUser defines interface checking ig user name in su list
type SuperUser interface {
	IsSuper(userName string) bool
}

// SenderChat is the sender of the message, sent on behalf of a chat. The
// channel itself for channel messages. The supergroup itself for messages
// from anonymous group administrators. The linked channel for messages
// automatically forwarded to the discussion group
type SenderChat struct {
	// ID is a unique identifier for this chat
	ID int64 `json:"id"`
	// the field below used only for logging purposes
	// UserName for private chats, supergroups and channels if available, optional
	UserName string `json:"username,omitempty"`
}

// Message is primary record to pass data from/to bots
type Message struct {
	ID         int
	From       User
	SenderChat SenderChat `json:"sender_chat,omitempty"`
	ChatID     int64
	Sent       time.Time
	HTML       string    `json:",omitempty"`
	Text       string    `json:",omitempty"`
	Entities   *[]Entity `json:",omitempty"`
	Image      *Image    `json:",omitempty"`
	ReplyTo    struct {
		From       User
		Text       string `json:",omitempty"`
		Sent       time.Time
		SenderChat SenderChat `json:"sender_chat,omitempty"`
	} `json:",omitempty"`
}

// Entity represents one special entity in a text message.
// For example, hashtags, usernames, URLs, etc.
type Entity struct {
	Type   string
	Offset int
	Length int
	URL    string `json:",omitempty"` // For “text_link” only, url that will be opened after user taps on the text
	User   *User  `json:",omitempty"` // For “text_mention” only, the mentioned user
}

// Image represents image
type Image struct {
	// FileID corresponds to Telegram file_id
	FileID   string
	Width    int
	Height   int
	Caption  string    `json:",omitempty"`
	Entities *[]Entity `json:",omitempty"`
}

// User defines user info of the Message
type User struct {
	ID          int64
	Username    string
	DisplayName string
}

func contains(s []string, e string) bool {
	e = strings.TrimSpace(e)
	for _, a := range s {
		if strings.EqualFold(a, e) {
			return true
		}
	}
	return false
}

func makeHTTPRequest(url string) (*http.Request, error) {
	req, err := http.NewRequest("GET", url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to make request %s: %w", url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// EscapeMarkDownV1Text escapes markdownV1 special characters, used in places where we want to send text as-is.
// For example, telegram username with underscores would be italicized if we don't escape it.
// https://core.telegram.org/bots/api#markdown-style
func EscapeMarkDownV1Text(text string) string {
	escSymbols := []string{"_", "*", "`", "["}
	for _, esc := range escSymbols {
		text = strings.Replace(text, esc, "\\"+esc, -1)
	}
	return text
}

// DisplayName returns user's display name or username or id
func DisplayName(msg Message) string {
	displayUsername := msg.From.DisplayName
	if displayUsername == "" {
		displayUsername = msg.From.Username
	}
	if displayUsername == "" {
		displayUsername = fmt.Sprintf("%d", msg.From.ID)
	}
	return displayUsername
}
