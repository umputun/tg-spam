package main

import (
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	_ "modernc.org/sqlite" // sqlite driver loaded here

	"github.com/umputun/tg-spam/lib/tgspam"

	"github.com/umputun/tg-spam/_examples/simplechat/storage"
	"github.com/umputun/tg-spam/_examples/simplechat/web"
)

func main() {
	slog.Info("Starting simple chat")

	// prepare storage
	db, err := sql.Open("sqlite", "messages.db")
	if err != nil {
		slog.Error("", slog.Any("error", err))
		os.Exit(1)
	}
	store, err := storage.NewMessages(db)
	if err != nil {
		slog.Error("", slog.Any("error", err))
		os.Exit(1)
	}

	// make spam detector
	detector := tgspam.NewDetector(tgspam.Config{
		MinMsgLen:           10,
		SimilarityThreshold: 0.8,
		MinSpamProbability:  50,
		HTTPClient:          &http.Client{Timeout: 5 * time.Second},
	})

	// load stop words from a file
	stopWords, err := os.Open("data/stop-words.txt")
	if err != nil {
		slog.Error("Error opening stop words file:", slog.Any("error", err))
		return
	}
	res, err := detector.LoadStopWords(stopWords)
	if err != nil {
		slog.Error("Error loading stop words:", slog.Any("error", err))
		return
	}
	slog.Info("Loaded ", slog.Any("stop words", res.StopWords))

	// load spam and ham samples from files
	spamSamples, err := os.Open("data/spam-samples.txt")
	if err != nil {
		slog.Error("Error opening spam samples file:", slog.Any("error", err))
		return
	}
	hamSamples, err := os.Open("data/ham-samples.txt")
	if err != nil {
		slog.Error("Error opening ham samples file:", slog.Any("error", err))
		return
	}
	excludedTokens, err := os.Open("data/exclude-tokens.txt")
	if err != nil {
		slog.Error("Error opening excluded tokens file:", slog.Any("error", err))
		return
	}
	res, err = detector.LoadSamples(excludedTokens, []io.Reader{spamSamples}, []io.Reader{hamSamples})
	if err != nil {
		slog.Error("Error loading samples:", slog.Any("error", err))
		return
	}

	slog.Info("Loaded ", slog.Any("spam samples", res.SpamSamples), slog.Any("ham samples", res.HamSamples))

	// prepare and start web server
	srv := &web.Server{
		Addr:     ":8080",
		Detector: detector,
		Storage:  store,
		UserCredentials: map[string]string{
			"user1": "password1",
			"user2": "password2",
			"u1":    "p1",
		},
	}
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("", slog.Any("error", err))
		os.Exit(1)
	}
}
