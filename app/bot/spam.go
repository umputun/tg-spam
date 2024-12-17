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

	"github.com/umputun/tg-spam/lib/approved"
	"github.com/umputun/tg-spam/lib/spamcheck"
	"github.com/umputun/tg-spam/lib/tgspam"
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
	Check(request spamcheck.Request) (spam bool, cr []spamcheck.Response)
	LoadSamples(exclReader io.Reader, spamReaders, hamReaders []io.Reader) (tgspam.LoadResult, error)
	LoadStopWords(readers ...io.Reader) (tgspam.LoadResult, error)
	UpdateSpam(msg string) error
	UpdateHam(msg string) error
	AddApprovedUser(user approved.UserInfo) error
	RemoveApprovedUser(id string) error
	ApprovedUsers() (res []approved.UserInfo)
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
func (s *SpamFilter) OnMessage(msg Message, checkOnly bool) (response Response) {
	if msg.From.ID == 0 { // don't check system messages
		return Response{}
	}
	displayUsername := DisplayName(msg)

	spamReq := spamcheck.Request{Msg: msg.Text, CheckOnly: checkOnly,
		UserID: strconv.FormatInt(msg.From.ID, 10), UserName: msg.From.Username}
	if msg.Image != nil {
		spamReq.Meta.Images = 1
	}
	if msg.WithVideo || msg.WithVideoNote {
		spamReq.Meta.HasVideo = true
	}
	if msg.WithForward {
		spamReq.Meta.HasForward = true
	}
	spamReq.Meta.Links = strings.Count(msg.Text, "http://") + strings.Count(msg.Text, "https://")
	isSpam, checkResults := s.Check(spamReq)
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
	if err := s.Detector.AddApprovedUser(approved.UserInfo{UserID: fmt.Sprintf("%d", id), UserName: name}); err != nil {
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
				if event.Op == fsnotify.Remove {
					// may happen when the file is renamed, ignore
					continue
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
	log.Printf("[DEBUG] remove dynamic spam sample: %q", sample)
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
	log.Printf("[DEBUG] remove dynamic ham sample: %q", sample)
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
func (s *SpamFilter) removeDynamicSample(msg, fileName string) (int, error) {
	spamDynamicReader, err := os.Open(fileName) //nolint:gosec // file name is not user input
	if err != nil {
		return 0, fmt.Errorf("failed to open spam dynamic file %s: %w", fileName, err)
	}
	defer spamDynamicReader.Close()

	// create a temporary file
	spamDynamicWriter, err := os.CreateTemp("", "spam-dynamic-*.txt")
	if err != nil {
		return 0, fmt.Errorf("failed to create temporary file for spam dynamic samples: %w", err)
	}
	defer spamDynamicWriter.Close()

	// read from the original file and write to a temporary file, skipping the message to remove
	scanner := bufio.NewScanner(spamDynamicReader)
	count := 0
	for scanner.Scan() {
		sample := scanner.Text()
		if sample != msg {
			if _, err = spamDynamicWriter.WriteString(sample + "\n"); err != nil {
				return 0, fmt.Errorf("failed to write to temporary spam dynamic file: %w", err)
			}
		} else {
			count++
		}
	}
	if err = scanner.Err(); err != nil {
		return 0, fmt.Errorf("failed to read spam dynamic file: %w", err)
	}

	// ensure all writes are flushed to disk
	if err = spamDynamicWriter.Sync(); err != nil {
		return 0, fmt.Errorf("failed to flush writes to temporary file: %w", err)
	}
	if err = spamDynamicWriter.Close(); err != nil {
		return 0, fmt.Errorf("failed to close temporary spam dynamic file: %w", err)
	}

	// get the file info of the original file to retrieve its permissions
	fileInfo, err := os.Stat(fileName)
	if err != nil {
		return 0, fmt.Errorf("failed to get file info of the original spam dynamic file: %w", err)
	}

	// set the permissions of the temporary file to match the original file
	if err = os.Chmod(spamDynamicWriter.Name(), fileInfo.Mode()); err != nil {
		return 0, fmt.Errorf("failed to set permissions of the temporary spam dynamic file: %w", err)
	}

	if count == 0 {
		// not found, no need to replace the original file
		if err := os.Remove(spamDynamicWriter.Name()); err != nil {
			// cleanup temporary file as we won't replace the original file with it
			return 0, fmt.Errorf("failed to remove temporary spam dynamic file: %w", err)
		}
		return 0, fmt.Errorf("sample %q not found in %s", msg, fileName)
	}

	// replace the original file with the temporary file
	if err := s.fileReplace(spamDynamicWriter.Name(), fileName, fileInfo.Mode()); err != nil {
		return 0, fmt.Errorf("failed to replace the original spam dynamic file with the temporary file: %w", err)
	}
	log.Printf("[DEBUG] removed %d samples from %s", count, fileName)
	return count, nil
}

// fileReplace copies the file content from src to dst and removes the src file.
// This function is used as a workaround for the "invalid cross-device link" error.
func (s *SpamFilter) fileReplace(src, dst string, perm os.FileMode) error {
	input, err := os.ReadFile(src) //nolint:gosec // file name is not user input
	if err != nil {
		return fmt.Errorf("failed to read from the source file: %w", err)
	}

	if err := os.WriteFile(dst, input, perm); err != nil {
		return fmt.Errorf("failed to write to the destination file: %w", err)
	}

	if err := os.Remove(src); err != nil {
		return fmt.Errorf("failed to remove the source file after copy: %w", err)
	}

	return nil
}
