package lib

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

//go:generate moq --out mocks/sample_updater.go --pkg mocks --skip-ensure . SampleUpdater
//go:generate moq --out mocks/http_client.go --pkg mocks --skip-ensure . HTTPClient

// Detector is a spam detector, thread-safe.
type Detector struct {
	Config
	classifier     Classifier
	tokenizedSpam  []map[string]int
	approvedUsers  map[int64]bool
	stopWords      []string
	excludedTokens []string

	spamSamplesUpd SampleUpdater
	hamSamplesUpd  SampleUpdater

	lock sync.RWMutex
}

// Config is a set of parameters for Detector.
type Config struct {
	SimilarityThreshold float64    // threshold for spam similarity, 0.0 - 1.0
	MinMsgLen           int        // minimum message length to check
	MaxAllowedEmoji     int        // maximum number of emojis allowed in a message
	CasAPI              string     // CAS API URL
	FirstMessageOnly    bool       // if true, only the first message from a user is checked
	HTTPClient          HTTPClient // http client to use for requests
}

// CheckResult is a result of spam check.
type CheckResult struct {
	Name    string // name of the check
	Spam    bool   // true if spam
	Details string // details of the check
}

// LoadResult is a result of loading samples.
type LoadResult struct {
	ExcludedTokens int // number of excluded tokens
	SpamSamples    int // number of spam samples
	HamSamples     int // number of ham samples
	StopWords      int // number of stop words (phrases)
}

// SampleUpdater is an interface for updating spam/ham samples on the fly.
type SampleUpdater interface {
	Append(msg string) error        // append a message to the samples storage
	Reader() (io.ReadCloser, error) // return a reader for the samples storage
}

// HTTPClient wrap http.Client to allow mocking
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// NewDetector makes a new Detector with the given config.
func NewDetector(p Config) *Detector {
	return &Detector{
		Config:        p,
		classifier:    NewClassifier(),
		approvedUsers: make(map[int64]bool),
		tokenizedSpam: []map[string]int{},
	}
}

// Check checks if a given message is spam. Returns true if spam.
// Also returns a list of check results.
func (d *Detector) Check(msg string, userID int64) (spam bool, cr []CheckResult) {

	if len([]rune(msg)) < d.MinMsgLen {
		return false, []CheckResult{{Name: "message length", Spam: false, Details: "too short"}}
	}

	d.lock.RLock()
	defer d.lock.RUnlock()

	if d.FirstMessageOnly && d.approvedUsers[userID] {
		return false, []CheckResult{{Name: "pre-approved", Spam: false, Details: "user already approved"}}
	}

	if len(d.stopWords) > 0 {
		cr = append(cr, d.isStopWord(msg))
	}

	if d.MaxAllowedEmoji >= 0 {
		cr = append(cr, d.isManyEmojis(msg))
	}

	if d.SimilarityThreshold > 0 && len(d.tokenizedSpam) > 0 {
		cr = append(cr, d.isSpamSimilarityHigh(msg))
	}

	if d.classifier.NAllDocument > 0 {
		cr = append(cr, d.isSpamClassified(msg))
	}

	if d.CasAPI != "" {
		cr = append(cr, d.isCasSpam(userID))
	}

	for _, r := range cr {
		if r.Spam {
			return true, cr
		}
	}

	if d.FirstMessageOnly {
		d.approvedUsers[userID] = true
	}

	return false, cr
}

// Reset resets spam samples/classifier, excluded tokens, stop words and approved users.
func (d *Detector) Reset() {
	d.lock.Lock()
	defer d.lock.Unlock()

	d.tokenizedSpam = []map[string]int{}
	d.excludedTokens = []string{}
	d.classifier.Reset()
	d.approvedUsers = make(map[int64]bool)
	d.stopWords = []string{}
}

// WithSpamUpdater sets a SampleUpdater for spam samples.
func (d *Detector) WithSpamUpdater(s SampleUpdater) {
	d.spamSamplesUpd = s
}

// WithHamUpdater sets a SampleUpdater for ham samples.
func (d *Detector) WithHamUpdater(s SampleUpdater) {
	d.hamSamplesUpd = s
}

// LoadSamples loads spam samples from a reader and updates the classifier.
// Reset spam, ham samples/classifier, and excluded tokens.
func (d *Detector) LoadSamples(exclReader io.Reader, spamReaders, hamReaders []io.Reader) (LoadResult, error) {
	d.lock.Lock()
	defer d.lock.Unlock()

	d.tokenizedSpam = []map[string]int{}
	d.excludedTokens = []string{}
	d.classifier.Reset()

	// excluded tokens should be loaded before spam samples
	for t := range d.tokenChan(exclReader) {
		d.excludedTokens = append(d.excludedTokens, strings.ToLower(t))
	}
	lr := LoadResult{ExcludedTokens: len(d.excludedTokens)}

	// load spam samples and update the classifier with them
	docs := []Document{}
	for token := range d.tokenChan(spamReaders...) {
		tokenizedSpam := d.tokenize(token)
		d.tokenizedSpam = append(d.tokenizedSpam, tokenizedSpam) // add to list of samples
		tokens := make([]string, 0, len(tokenizedSpam))
		for token := range tokenizedSpam {
			tokens = append(tokens, token)
		}
		docs = append(docs, Document{Class: "spam", Tokens: tokens})
		lr.SpamSamples++
	}

	for token := range d.tokenChan(hamReaders...) {
		tokenizedSpam := d.tokenize(token)
		tokens := make([]string, 0, len(tokenizedSpam))
		for token := range tokenizedSpam {
			tokens = append(tokens, token)
		}
		docs = append(docs, Document{Class: "ham", Tokens: tokens})
		lr.HamSamples++
	}

	d.classifier.Learn(docs...)

	return lr, nil
}

// LoadStopWords loads stop words from a reader. Reset stop words list before loading.
func (d *Detector) LoadStopWords(readers ...io.Reader) (LoadResult, error) {
	d.lock.Lock()
	defer d.lock.Unlock()
	d.stopWords = []string{}
	for t := range d.tokenChan(readers...) {
		d.stopWords = append(d.stopWords, strings.ToLower(t))
	}
	log.Printf("[INFO] loaded %d stop words", len(d.stopWords))
	return LoadResult{StopWords: len(d.stopWords)}, nil
}

// UpdateSpam appends a message to the spam samples file and updates the classifier
// doesn't reset state, update append spam samples
func (d *Detector) UpdateSpam(msg string) error {
	d.lock.Lock()
	defer d.lock.Unlock()

	if d.spamSamplesUpd == nil {
		return nil
	}

	// write to dynamic samples storage
	if err := d.spamSamplesUpd.Append(msg); err != nil {
		return fmt.Errorf("can't update spam samples: %w", err)
	}

	// load spam samples and update the classifier with them
	docs := []Document{}
	for token := range d.tokenChan(bytes.NewBufferString(msg)) {
		tokenizedSpam := d.tokenize(token)
		d.tokenizedSpam = append(d.tokenizedSpam, tokenizedSpam) // add to list of samples
		tokens := make([]string, 0, len(tokenizedSpam))
		for token := range tokenizedSpam {
			tokens = append(tokens, token)
		}
		docs = append(docs, Document{Class: "spam", Tokens: tokens})
	}
	d.classifier.Learn(docs...)
	return nil
}

// UpdateHam appends a message to the ham samples file and updates the classifier
// doesn't reset state, update append ham samples
func (d *Detector) UpdateHam(msg string) error {
	d.lock.Lock()
	defer d.lock.Unlock()

	if d.hamSamplesUpd == nil {
		return nil
	}

	if err := d.hamSamplesUpd.Append(msg); err != nil {
		return fmt.Errorf("can't update ham samples: %w", err)
	}

	// load ham samples and update the classifier with them
	docs := []Document{}
	for token := range d.tokenChan(bytes.NewBufferString(msg)) {
		tokenizedHam := d.tokenize(token)
		tokens := make([]string, 0, len(tokenizedHam))
		for token := range tokenizedHam {
			tokens = append(tokens, token)
		}
		docs = append(docs, Document{Class: "ham", Tokens: tokens})
	}
	d.classifier.Learn(docs...)
	return nil
}

// tokenChan parses readers and returns a channel of tokens.
// A line per-token or comma-separated "tokens" supported
func (d *Detector) tokenChan(readers ...io.Reader) <-chan string {
	resCh := make(chan string)

	go func() {
		defer close(resCh)

		for _, reader := range readers {
			scanner := bufio.NewScanner(reader)
			for scanner.Scan() {
				line := scanner.Text()
				if strings.Contains(line, ",") && strings.HasPrefix(line, "\"") {
					// line with comma-separated tokens
					lineTokens := strings.Split(line, ",")
					for _, token := range lineTokens {
						cleanToken := strings.Trim(token, " \"\n\r\t")
						if cleanToken != "" {
							resCh <- cleanToken
						}
					}
					continue
				}
				// each line with a single token
				cleanToken := strings.Trim(line, " \n\r\t")
				if cleanToken != "" {
					resCh <- cleanToken
				}
			}

			if err := scanner.Err(); err != nil {
				log.Printf("[WARN] failed to read tokens, error=%v", err)
			}
		}
	}()

	return resCh
}

// ApprovedUsers returns a list of approved users.
func (d *Detector) ApprovedUsers() (res []int64) {
	d.lock.RLock()
	defer d.lock.RUnlock()
	res = make([]int64, 0, len(d.approvedUsers))
	for userID := range d.approvedUsers {
		res = append(res, userID)
	}
	return res
}

// LoadApprovedUsers loads a list of approved users from a reader.
// Reset approved users list before loading. It expects a list of user IDs (int64) from the reader, one per line.
func (d *Detector) LoadApprovedUsers(r io.Reader) (count int, err error) {
	d.lock.Lock()
	defer d.lock.Unlock()
	d.approvedUsers = make(map[int64]bool)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		userID := scanner.Text()
		id, err := strconv.ParseInt(userID, 10, 64)
		if err != nil {
			return count, fmt.Errorf("failed to parse user id %q: %w", userID, err)
		}
		d.approvedUsers[id] = true
		count++
	}

	return count, scanner.Err()
}

// tokenize takes a string and returns a map where the keys are unique words (tokens)
// and the values are the frequencies of those words in the string.
// exclude tokens representing common words.
func (d *Detector) tokenize(inp string) map[string]int {
	isExcludedToken := func(token string) bool {
		for _, w := range d.excludedTokens {
			if strings.EqualFold(token, w) {
				return true
			}
		}
		return false
	}

	tokenFrequency := make(map[string]int)
	tokens := strings.Fields(inp)
	for _, token := range tokens {
		if isExcludedToken(token) {
			continue
		}
		token = cleanEmoji(token)
		token = strings.Trim(token, ".,!?-:;()#")
		token = strings.ToLower(token)
		if len([]rune(token)) < 3 {
			continue
		}
		tokenFrequency[strings.ToLower(token)]++
	}
	return tokenFrequency
}

// isSpam checks if a given message is similar to any of the known bad messages.
func (d *Detector) isSpamSimilarityHigh(msg string) CheckResult {
	// check for spam similarity
	tokenizedMessage := d.tokenize(msg)
	maxSimilarity := 0.0
	for _, spam := range d.tokenizedSpam {
		similarity := d.cosineSimilarity(tokenizedMessage, spam)
		if similarity > maxSimilarity {
			maxSimilarity = similarity
		}
		if similarity >= d.SimilarityThreshold {
			return CheckResult{Spam: true, Name: "similarity",
				Details: fmt.Sprintf("%0.2f/%0.2f", maxSimilarity, d.SimilarityThreshold)}
		}
	}
	return CheckResult{Spam: false, Name: "similarity", Details: fmt.Sprintf("%0.2f/%0.2f", maxSimilarity, d.SimilarityThreshold)}
}

// cosineSimilarity calculates the cosine similarity between two token frequency maps.
func (d *Detector) cosineSimilarity(a, b map[string]int) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}

	dotProduct := 0      // sum of product of corresponding frequencies
	normA, normB := 0, 0 // square root of sum of squares of frequencies

	for key, val := range a {
		dotProduct += val * b[key]
		normA += val * val
	}
	for _, val := range b {
		normB += val * val
	}

	if normA == 0 || normB == 0 {
		return 0.0
	}

	// cosine similarity formula
	return float64(dotProduct) / (math.Sqrt(float64(normA)) * math.Sqrt(float64(normB)))
}

func (d *Detector) isCasSpam(msgID int64) CheckResult {
	reqURL := fmt.Sprintf("%s/check?user_id=%d", d.CasAPI, msgID)
	req, err := http.NewRequest("GET", reqURL, http.NoBody)
	if err != nil {
		return CheckResult{Spam: false, Name: "cas", Details: fmt.Sprintf("failed to make request %s: %v", reqURL, err)}
	}

	resp, err := d.HTTPClient.Do(req)
	if err != nil {
		return CheckResult{Spam: false, Name: "cas", Details: fmt.Sprintf("ffailed to send request %s: %v", reqURL, err)}
	}
	defer resp.Body.Close()

	respData := struct {
		OK          bool   `json:"ok"` // ok means user is a spammer
		Description string `json:"description"`
	}{}

	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		return CheckResult{Spam: false, Name: "cas", Details: fmt.Sprintf("failed to parse response from %s: %v", reqURL, err)}
	}

	if respData.OK {
		return CheckResult{Name: "cas", Spam: true, Details: respData.Description}
	}
	details := respData.Description
	if details == "" {
		details = "not found"
	}
	return CheckResult{Name: "cas", Spam: false, Details: details}
}

func (d *Detector) isSpamClassified(msg string) CheckResult {
	// Classify tokens from a document
	tm := d.tokenize(msg)
	tokens := make([]string, 0, len(tm))
	for token := range tm {
		tokens = append(tokens, token)
	}
	allScores, class, certain := d.classifier.Classify(tokens...)
	return CheckResult{Name: "classifier", Spam: class == "spam" && certain,
		Details: fmt.Sprintf("spam:%.4f, ham:%.4f", allScores["spam"], allScores["ham"])}
}

func (d *Detector) isStopWord(msg string) CheckResult {
	cleanMsg := cleanEmoji(strings.ToLower(msg))
	for _, word := range d.stopWords {
		if strings.Contains(cleanMsg, strings.ToLower(word)) {
			return CheckResult{Name: "stopword", Spam: true, Details: word}
		}
	}
	return CheckResult{Name: "stopword", Spam: false}
}

func (d *Detector) isManyEmojis(msg string) CheckResult {
	count := countEmoji(msg)
	return CheckResult{Name: "emoji", Spam: count > d.MaxAllowedEmoji, Details: fmt.Sprintf("%d/%d", count, d.MaxAllowedEmoji)}
}
