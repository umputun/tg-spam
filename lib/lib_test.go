package lib_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/umputun/tg-spam/lib/spamcheck"
	"github.com/umputun/tg-spam/lib/tgspam"
)

// ExampleNewDetector demonstrates how to initialize a new Detector and use it to check a message for spam.
func ExampleNewDetector() {
	// Initialize a new Detector with a Config
	detector := tgspam.NewDetector(tgspam.Config{
		MaxAllowedEmoji:  5,
		MinMsgLen:        10,
		FirstMessageOnly: true,
		CasAPI:           "https://cas.example.com",
		HTTPClient:       &http.Client{},
	})

	// Load stop words
	stopWords := strings.NewReader("\"word1\"\n\"word2\"\n\"hello world\"\n\"some phrase\", \"another phrase\"")
	res, err := detector.LoadStopWords(stopWords)
	if err != nil {
		fmt.Println("Error loading stop words:", err)
		return
	}
	fmt.Println("Loaded", res.StopWords, "stop words")

	// Load spam and ham samples
	spamSamples := strings.NewReader("spam sample 1\nspam sample 2\nspam sample 3")
	hamSamples := strings.NewReader("ham sample 1\nham sample 2\nham sample 3")
	excludedTokens := strings.NewReader("\"the\", \"a\", \"an\"")
	res, err = detector.LoadSamples(excludedTokens, []io.Reader{spamSamples}, []io.Reader{hamSamples})
	if err != nil {
		fmt.Println("Error loading samples:", err)
		return
	}
	fmt.Println("Loaded", res.SpamSamples, "spam samples and", res.HamSamples, "ham samples")

	// check a message for spam
	isSpam, info := detector.Check(spamcheck.Request{Msg: "This is a test message", UserID: "user1", UserName: "John Doe"})
	if isSpam {
		fmt.Println("The message is spam, info:", info)
	} else {
		fmt.Println("The message is not spam, info:", info)
	}
}
