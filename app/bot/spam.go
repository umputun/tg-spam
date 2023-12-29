package bot

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/hashicorp/go-multierror"

	"github.com/umputun/tg-spam/lib"
)

//go:generate moq --out mocks/detector.go --pkg mocks --skip-ensure --with-resets . Detector

// SpamFilter bot checks if a user is a spammer using lib.Detector
// Reloads spam samples, stop words and excluded tokens on file change.
type SpamFilter struct {
	Detector
	params SpamConfig
}

// SpamConfig is a full set of parameters for spam bot
type SpamConfig struct {

	// samples file names need to be watched for changes and reload.
	SpamSamplesFile    string
	HamSamplesFile     string
	StopWordsFile      string
	ExcludedTokensFile string
	SpamDynamicFile    string
	HamDynamicFile     string

	SpamMsg    string
	SpamDryMsg string

	WatchDelay time.Duration

	Dry bool
}

// Detector is a spam detector interface
type Detector interface {
	Check(request lib.CheckRequest) (spam bool, cr []lib.CheckResult)
	LoadSamples(exclReader io.Reader, spamReaders, hamReaders []io.Reader) (lib.LoadResult, error)
	LoadStopWords(readers ...io.Reader) (lib.LoadResult, error)
	UpdateSpam(msg string) error
	UpdateHam(msg string) error
	AddApprovedUser(user lib.UserInfo) error
	RemoveApprovedUser(id string) error
	ApprovedUsers() (res []lib.UserInfo)
	IsApprovedUser(userID string) bool
}

// NewSpamFilter creates new spam filter
func NewSpamFilter(ctx context.Context, detector Detector, params SpamConfig) *SpamFilter {
	res := &SpamFilter{Detector: detector, params: params}
	go func() {
		if err := res.watch(ctx, params.WatchDelay); err != nil {
			log.Printf("[WARN] samples file watcher failed: %v", err)
		}
	}()
	return res
}

// OnMessage checks if user already approved and if not checks if user is a spammer
func (s *SpamFilter) OnMessage(msg Message) (response Response) {
	if msg.From.ID == 0 { // don't check system messages
		return Response{}
	}
	displayUsername := DisplayName(msg)
	isSpam, checkResults := s.Check(lib.CheckRequest{
		Msg: msg.Text, UserID: strconv.FormatInt(msg.From.ID, 10), UserName: msg.From.Username})
	crs := []string{}
	for _, cr := range checkResults {
		crs = append(crs, fmt.Sprintf("{name: %s, spam: %v, details: %s}", cr.Name, cr.Spam, cr.Details))
	}
	checkResultStr := strings.Join(crs, ", ")
	if isSpam {
		log.Printf("[INFO] user %s detected as spammer: %s, %q", displayUsername, checkResultStr, msg.Text)
		msgPrefix := s.params.SpamMsg
		if s.params.Dry {
			msgPrefix = s.params.SpamDryMsg
		}
		spamRespMsg := fmt.Sprintf("%s: %q (%d)", msgPrefix, displayUsername, msg.From.ID)
		return Response{Text: spamRespMsg, Send: true, ReplyTo: msg.ID, BanInterval: PermanentBanDuration, CheckResults: checkResults,
			DeleteReplyTo: true, User: User{Username: msg.From.Username, ID: msg.From.ID, DisplayName: msg.From.DisplayName},
		}
	}
	log.Printf("[DEBUG] user %s is not a spammer, %s", displayUsername, checkResultStr)
	return Response{CheckResults: checkResults} // not a spam
}

// UpdateSpam appends a message to the spam samples file and updates the classifier
func (s *SpamFilter) UpdateSpam(msg string) error {
	cleanMsg := strings.ReplaceAll(msg, "\n", " ")
	log.Printf("[DEBUG] update spam samples with %q", cleanMsg)
	if err := s.Detector.UpdateSpam(cleanMsg); err != nil {
		return fmt.Errorf("can't update spam samples: %w", err)
	}
	return nil
}

// UpdateHam appends a message to the ham samples file and updates the classifier
func (s *SpamFilter) UpdateHam(msg string) error {
	cleanMsg := strings.ReplaceAll(msg, "\n", " ")
	log.Printf("[DEBUG] update ham samples with %q", cleanMsg)
	if err := s.Detector.UpdateHam(cleanMsg); err != nil {
		return fmt.Errorf("can't update ham samples: %w", err)
	}
	return nil
}

// IsApprovedUser checks if user is in the list of approved users
func (s *SpamFilter) IsApprovedUser(userID int64) bool {
	return s.Detector.IsApprovedUser(fmt.Sprintf("%d", userID))
}

// AddApprovedUser adds users to the list of approved users, to both the detector and the storage
func (s *SpamFilter) AddApprovedUser(id int64, name string) error {
	log.Printf("[INFO] add aproved user: id:%d, name:%q", id, name)
	if err := s.Detector.AddApprovedUser(lib.UserInfo{UserID: fmt.Sprintf("%d", id), UserName: name}); err != nil {
		return fmt.Errorf("failed to write approved user to storage: %w", err)
	}
	return nil
}

// RemoveApprovedUser removes users from the list of approved users in both the detector and the storage
func (s *SpamFilter) RemoveApprovedUser(id int64) error {
	log.Printf("[INFO] remove aproved user: %d", id)
	if err := s.Detector.RemoveApprovedUser(fmt.Sprintf("%d", id)); err != nil {
		return fmt.Errorf("failed to delete approved user from storage: %w", err)
	}
	return nil
}

// watch watches for changes in samples files and reloads them
// delay is a time to wait after the last change before reloading to avoid multiple reloads
func (s *SpamFilter) watch(ctx context.Context, delay time.Duration) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}
	defer watcher.Close()

	done := make(chan bool)
	reloadTimer := time.NewTimer(delay)
	reloadPending := false

	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				log.Printf("[INFO] stopping watcher for samples: %v", ctx.Err())
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				log.Printf("[DEBUG] file %q updated, op: %v", event.Name, event.Op)
				if !reloadPending {
					reloadPending = true
					reloadTimer.Reset(delay)
				}
			case <-reloadTimer.C:
				if reloadPending {
					reloadPending = false
					if err := s.ReloadSamples(); err != nil {
						log.Printf("[WARN] %v", err)
					}
				}
			case e, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("[WARN] watcher error: %v", e)
			}
		}
	}()

	errs := new(multierror.Error)
	addToWatcher := func(file string) error {
		if _, err := os.Stat(file); err != nil {
			return fmt.Errorf("failed to stat file %q: %w", file, err)
		}
		log.Printf("[DEBUG] add file %q to watcher", file)
		return watcher.Add(file)
	}
	errs = multierror.Append(errs, addToWatcher(s.params.ExcludedTokensFile))
	errs = multierror.Append(errs, addToWatcher(s.params.SpamSamplesFile))
	errs = multierror.Append(errs, addToWatcher(s.params.HamSamplesFile))
	errs = multierror.Append(errs, addToWatcher(s.params.StopWordsFile))
	if err := errs.ErrorOrNil(); err != nil {
		return fmt.Errorf("failed to add some files to watcher: %w", err)
	}
	<-done
	return nil
}

// ReloadSamples reloads samples and stop-words
func (s *SpamFilter) ReloadSamples() (err error) {
	log.Printf("[DEBUG] reloading samples")

	var exclReader, spamReader, hamReader, stopWordsReader, spamDynamicReader, hamDynamicReader io.ReadCloser

	// open mandatory spam and ham samples files
	if spamReader, err = os.Open(s.params.SpamSamplesFile); err != nil {
		return fmt.Errorf("failed to open spam samples file %q: %w", s.params.SpamSamplesFile, err)
	}
	defer spamReader.Close()

	if hamReader, err = os.Open(s.params.HamSamplesFile); err != nil {
		return fmt.Errorf("failed to open ham samples file %q: %w", s.params.HamSamplesFile, err)
	}
	defer hamReader.Close()

	// stop-words are optional
	if stopWordsReader, err = os.Open(s.params.StopWordsFile); err != nil {
		stopWordsReader = io.NopCloser(bytes.NewReader([]byte("")))
	}
	defer stopWordsReader.Close()

	// excluded tokens are optional
	if exclReader, err = os.Open(s.params.ExcludedTokensFile); err != nil {
		exclReader = io.NopCloser(bytes.NewReader([]byte("")))
	}
	defer exclReader.Close()

	// dynamic samples are optional
	if spamDynamicReader, err = os.Open(s.params.SpamDynamicFile); err != nil {
		spamDynamicReader = io.NopCloser(bytes.NewReader([]byte("")))
	}
	defer spamDynamicReader.Close()

	if hamDynamicReader, err = os.Open(s.params.HamDynamicFile); err != nil {
		hamDynamicReader = io.NopCloser(bytes.NewReader([]byte("")))
	}
	defer hamDynamicReader.Close()

	// reload samples and stop-words. note: we don't need reset as LoadSamples and LoadStopWords clear the state first
	lr, err := s.LoadSamples(exclReader, []io.Reader{spamReader, spamDynamicReader},
		[]io.Reader{hamReader, hamDynamicReader})
	if err != nil {
		return fmt.Errorf("failed to reload samples: %w", err)
	}

	ls, err := s.LoadStopWords(stopWordsReader)
	if err != nil {
		return fmt.Errorf("failed to reload stop words: %w", err)
	}

	log.Printf("[INFO] loaded samples - spam: %d, ham: %d, excluded tokens: %d, stop-words: %d",
		lr.SpamSamples, lr.HamSamples, lr.ExcludedTokens, ls.StopWords)

	return nil
}

// DynamicSamples returns dynamic spam and ham samples. both are optional
func (s *SpamFilter) DynamicSamples() (spam, ham []string, err error) {
	errs := new(multierror.Error)

	if spamDynamicReader, err := os.Open(s.params.SpamDynamicFile); err != nil {
		spam = []string{}
	} else {
		defer spamDynamicReader.Close()
		scanner := bufio.NewScanner(spamDynamicReader)
		for scanner.Scan() {
			spam = append(spam, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("failed to read spam dynamic file: %w", err))
		}
	}

	if hamDynamicReader, err := os.Open(s.params.HamDynamicFile); err != nil {
		ham = []string{}
	} else {
		defer hamDynamicReader.Close()
		scanner := bufio.NewScanner(hamDynamicReader)
		for scanner.Scan() {
			ham = append(ham, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("failed to read ham dynamic file: %w", err))
		}
	}

	return spam, ham, errs.ErrorOrNil()
}

// RemoveDynamicSpamSample removes a sample from the spam dynamic samples file and reloads samples after this
func (s *SpamFilter) RemoveDynamicSpamSample(sample string) (int, error) {
	count, err := s.removeDynamicSample(sample, s.params.SpamDynamicFile)
	if err != nil {
		return 0, fmt.Errorf("failed to remove dynamic spam sample: %w", err)
	}
	if err := s.ReloadSamples(); err != nil {
		return 0, fmt.Errorf("failed to reload samples after removing dynamic spam sample: %w", err)
	}
	return count, nil
}

// RemoveDynamicHamSample removes a sample from the ham dynamic samples file and reloads samples after this
func (s *SpamFilter) RemoveDynamicHamSample(sample string) (int, error) {
	count, err := s.removeDynamicSample(sample, s.params.HamDynamicFile)
	if err != nil {
		return 0, fmt.Errorf("failed to remove dynamic ham sample: %w", err)
	}
	if err := s.ReloadSamples(); err != nil {
		return 0, fmt.Errorf("failed to reload samples after removing dynamic ham sample: %w", err)
	}
	return count, nil
}

// removeDynamicSample removes a sample from the spam dynamic samples file and reloads samples after this
//
//nolint:gosec // potential inclusion is fine here
func (s *SpamFilter) removeDynamicSample(msg, fileName string) (int, error) {
	spamDynamicReader, err := os.Open(fileName)
	if err != nil {
		return 0, fmt.Errorf("failed to open spam dynamic file %s: %w", fileName, err)
	}
	defer spamDynamicReader.Close()
	// read all samples, remove the one we need and write the rest back
	scanner := bufio.NewScanner(spamDynamicReader)
	count := 0
	var samples []string
	for scanner.Scan() {
		s := scanner.Text()
		if s != msg {
			samples = append(samples, s)
		} else {
			count++
		}
	}
	if err = scanner.Err(); err != nil {
		return 0, fmt.Errorf("failed to read spam dynamic file: %w", err)
	}
	if count == 0 {
		return 0, fmt.Errorf("sample %q not found in %s", msg, fileName)
	}

	// write samples back
	if err = spamDynamicReader.Close(); err != nil {
		return 0, fmt.Errorf("failed to close spam dynamic file: %w", err)
	}

	spamDynamicWriter, err := os.Create(fileName)
	if err != nil {
		return 0, fmt.Errorf("failed to open spam dynamic file for writing: %w", err)
	}
	defer spamDynamicWriter.Close()
	for _, s := range samples {
		if _, err := spamDynamicWriter.WriteString(s + "\n"); err != nil {
			return 0, fmt.Errorf("failed to write to spam dynamic file: %w", err)
		}
	}
	return count, nil
}
