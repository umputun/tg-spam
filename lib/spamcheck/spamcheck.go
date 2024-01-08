package spamcheck

import "fmt"

// Request is a request to check a message for spam.
type Request struct {
	Msg      string   `json:"msg"`       // message to check
	UserID   string   `json:"user_id"`   // user id
	UserName string   `json:"user_name"` // user name
	Meta     MetaData `json:"meta"`      // meta-info, provided by the client
}

// MetaData is a meta-info about the message, provided by the client.
type MetaData struct {
	Images int `json:"images"` // number of images in the message
	Links  int `json:"links"`  // number of links in the message
}

func (r *Request) String() string {
	return fmt.Sprintf("msg:%q, user:%q, id:%s, images:%d, links:%d",
		r.Msg, r.UserName, r.UserID, r.Meta.Images, r.Meta.Links)
}

// Response is a result of spam check.
type Response struct {
	Name    string `json:"name"`    // name of the check
	Spam    bool   `json:"spam"`    // true if spam
	Details string `json:"details"` // details of the check
}

func (r *Response) String() string {
	spamOrHam := "ham"
	if r.Spam {
		spamOrHam = "spam"
	}
	return fmt.Sprintf("%s: %s, %s", r.Name, spamOrHam, r.Details)
}
