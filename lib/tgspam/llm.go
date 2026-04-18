package tgspam

import (
	"context"
	"fmt"
	"strings"

	"github.com/umputun/tg-spam/lib/spamcheck"
)

type llmResponse struct {
	IsSpam     bool   `json:"spam"`
	Reason     string `json:"reason"`
	Confidence int    `json:"confidence"`
}

func runLLMProviderCheck(ctx context.Context, name, errorPrefix string, retryCount int, msg string, history []spamcheck.Request,
	send func(context.Context, string) (llmResponse, error),
) (spam bool, cr spamcheck.Response) {
	if retryCount < 1 {
		retryCount = 1
	}

	msg = appendHistoryToLLMMessage(msg, history)

	var resp llmResponse
	var err error
	for i := 0; i < retryCount; i++ {
		if resp, err = send(ctx, msg); err == nil {
			break
		}
		if ctx.Err() != nil {
			err = ctx.Err()
			break
		}
	}
	if err != nil {
		return false, spamcheck.Response{
			Spam: false, Name: name, Details: fmt.Sprintf("%s error: %v", errorPrefix, err), Error: err,
		}
	}

	return resp.IsSpam, spamcheck.Response{
		Spam: resp.IsSpam, Name: name,
		Details: strings.TrimSuffix(resp.Reason, ".") + ", confidence: " + fmt.Sprintf("%d%%", resp.Confidence),
	}
}

func appendHistoryToLLMMessage(msg string, history []spamcheck.Request) string {
	if len(history) == 0 {
		return msg
	}

	hist := make([]string, 0, len(history))
	for _, h := range history {
		hist = append(hist, fmt.Sprintf("%q: %q", h.UserName, h.Msg))
	}

	return fmt.Sprintf("User message:\n%s\n\nHistory:\n%s\n", msg, strings.Join(hist, "\n"))
}
