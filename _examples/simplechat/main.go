package main

import (
	"database/sql"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	_ "modernc.org/sqlite" // sqlite driver loaded here

	"github.com/umputun/tg-spam/lib/tgspam"

	"github.com/umputun/tg-spam/_examples/simplechat/storage"
	"github.com/umputun/tg-spam/_examples/simplechat/web"
)

func main() {
	log.Println("Starting simple chat")

	// prepare storage
	db, err := sql.Open("sqlite", "messages.db")
	if err != nil {
		log.Fatal(err)
	}
	store, err := storage.NewMessages(db)
	if err != nil {
		log.Fatal(err)
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
		log.Println("Error opening stop words file:", err)
		return
	}
	res, err := detector.LoadStopWords(stopWords)
	if err != nil {
		log.Println("Error loading stop words:", err)
		return
	}
	log.Printf("Loaded %d stop words", res.StopWords)

	// load spam and ham samples from files
	spamSamples, err := os.Open("data/spam-samples.txt")
	if err != nil {
		log.Println("Error opening spam samples file:", err)
		return
	}
	hamSamples, err := os.Open("data/ham-samples.txt")
	if err != nil {
		log.Println("Error opening ham samples file:", err)
		return
	}
	excludedTokens, err := os.Open("data/exclude-tokens.txt")
	if err != nil {
		log.Println("Error opening excluded tokens file:", err)
		return
	}
	res, err = detector.LoadSamples(excludedTokens, []io.Reader{spamSamples}, []io.Reader{hamSamples})
	if err != nil {
		log.Println("Error loading samples:", err)
		return
	}
	log.Printf("Loaded %d spam samples and %d ham samples", res.SpamSamples, res.HamSamples)

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
		log.Fatal(err)
	}
}
