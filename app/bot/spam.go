package bot

import (
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

//go:generate moq --out mocks/detector.go --pkg mocks --skip-ensure . Detector

// SpamFilter bot checks if a user is a spammer using lib.Detector
// Reloads spam samples, stop words and excluded tokens on file change.
type SpamFilter struct {
	director Detector
	params   SpamConfig

	// spamSamplesUpd sampleUpdaterInterface
	// hamSamplesUpd  sampleUpdaterInterface
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
	Check(msg string, userID string) (spam bool, cr []lib.CheckResult)
	LoadSamples(exclReader io.Reader, spamReaders, hamReaders []io.Reader) (lib.LoadResult, error)
	LoadStopWords(readers ...io.Reader) (lib.LoadResult, error)
	UpdateSpam(msg string) error
	UpdateHam(msg string) error
}

// NewSpamFilter creates new spam filter
func NewSpamFilter(ctx context.Context, detector Detector, params SpamConfig) *SpamFilter {
	res := &SpamFilter{director: detector, params: params}
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
	isSpam, checkResults := s.director.Check(msg.Text, strconv.FormatInt(msg.From.ID, 10))
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
		return Response{Text: spamRespMsg, Send: true, ReplyTo: msg.ID, BanInterval: PermanentBanDuration,
			DeleteReplyTo: true, User: User{Username: msg.From.Username, ID: msg.From.ID, DisplayName: msg.From.DisplayName},
		}
	}
	log.Printf("[DEBUG] user %s is not a spammer, %s", displayUsername, checkResultStr)
	return Response{} // not a spam
}

// UpdateSpam appends a message to the spam samples file and updates the classifier
func (s *SpamFilter) UpdateSpam(msg string) error {
	log.Printf("[DEBUG] update spam samples with %q", msg)
	if err := s.director.UpdateSpam(msg); err != nil {
		return fmt.Errorf("can't update spam samples: %w", err)
	}
	return nil
}

// UpdateHam appends a message to the ham samples file and updates the classifier
func (s *SpamFilter) UpdateHam(msg string) error {
	log.Printf("[DEBUG] update ham samples with %q", msg)
	if err := s.director.UpdateHam(msg); err != nil {
		return fmt.Errorf("can't update ham samples: %w", err)
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

	// reload samples and stop-words. note: we don't need Reset as LoadSamples and LoadStopWords clear the state first
	lr, err := s.director.LoadSamples(exclReader, []io.Reader{spamReader, spamDynamicReader},
		[]io.Reader{hamReader, hamDynamicReader})
	if err != nil {
		return fmt.Errorf("failed to reload samples: %w", err)
	}

	ls, err := s.director.LoadStopWords(stopWordsReader)
	if err != nil {
		return fmt.Errorf("failed to reload stop words: %w", err)
	}

	log.Printf("[INFO] loaded samples - spam: %d, ham: %d, excluded tokens: %d, stop-words: %d",
		lr.SpamSamples, lr.HamSamples, lr.ExcludedTokens, ls.StopWords)

	return nil
}
