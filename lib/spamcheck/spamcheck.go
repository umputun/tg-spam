package spamcheck

import (
	"fmt"
	"strings"
)

// Request is a request to check a message for spam.
type Request struct {
	Msg       string   `json:"msg"`        // message to check
	UserID    string   `json:"user_id"`    // user id
	UserName  string   `json:"user_name"`  // user name
	Meta      MetaData `json:"meta"`       // meta-info, provided by the client
	CheckOnly bool     `json:"check_only"` // if true, only check the message, do not write newly approved user to the database
}

// MetaData is a meta-info about the message, provided by the client.
type MetaData struct {
	Images     int  `json:"images"`      // number of images in the message
	Links      int  `json:"links"`       // number of links in the message
	HasVideo   bool `json:"has_video"`   // true if the message has a video or video note
	HasForward bool `json:"has_forward"` // true if the message has a forward
}

func (r *Request) String() string {
	return fmt.Sprintf("msg:%q, user:%q, id:%s, images:%d, links:%d, has_video:%v",
		r.Msg, r.UserName, r.UserID, r.Meta.Images, r.Meta.Links, r.Meta.HasVideo)
}

// Response is a result of spam check.
type Response struct {
	Name    string `json:"name"`    // name of the check
	Spam    bool   `json:"spam"`    // true if spam
	Details string `json:"details"` // details of the check
	Error   error  `json:"-"`       // error message, if any. Do not serialize it
}

func (r *Response) String() string {
	spamOrHam := "ham"
	if r.Spam {
		spamOrHam = "spam"
	}
	return fmt.Sprintf("%s: %s, %s", r.Name, spamOrHam, r.Details)
}

// ChecksToString converts a slice of checks to a string
func ChecksToString(checks []Response) string {
	elems := []string{}
	for _, r := range checks {
		elems = append(elems, "{"+r.String()+"}")

	}
	return fmt.Sprintf("[%s] ", strings.Join(elems, ", "))
}
