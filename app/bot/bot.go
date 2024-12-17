package bot

import (
	"fmt"
	"strings"
	"time"

	"github.com/umputun/tg-spam/lib/spamcheck"
)

// PermanentBanDuration defines duration of permanent ban:
// If user is restricted for more than 366 days or less than 30 seconds from the current time,
// they are considered to be restricted forever.
var PermanentBanDuration = time.Hour * 24 * 400

// Response describes bot's reaction on particular message
type Response struct {
	Text          string
	Send          bool                 // status
	BanInterval   time.Duration        // bots banning user set the interval
	User          User                 // user to ban
	ChannelID     int64                // channel to ban, if set then User and BanInterval are ignored
	ReplyTo       int                  // message to reply to, if 0 then no reply but common message
	DeleteReplyTo bool                 // delete message what bot replays to
	CheckResults  []spamcheck.Response // check results for the message
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

	WithVideo     bool `json:",omitempty"`
	WithVideoNote bool `json:",omitempty"`
	WithForward   bool `json:",omitempty"`
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
	ID          int64  `json:"id"`
	Username    string `json:"user_name,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
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
	return strings.TrimSpace(displayUsername)
}
