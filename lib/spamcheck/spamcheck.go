package spamcheck

import "fmt"

// Request is a request to check a message for spam.
type Request struct {
	Msg      string `json:"msg"`
	UserID   string `json:"user_id"`
	UserName string `json:"user_name"`
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
