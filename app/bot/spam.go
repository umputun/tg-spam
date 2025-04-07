package bot

import (
	"context"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"

	"github.com/hashicorp/go-multierror"

	"github.com/umputun/tg-spam/app/storage"
	"github.com/umputun/tg-spam/lib/approved"
	"github.com/umputun/tg-spam/lib/spamcheck"
	"github.com/umputun/tg-spam/lib/tgspam"
)

//go:generate moq --out mocks/detector.go --pkg mocks --skip-ensure --with-resets . Detector
//go:generate moq --out mocks/samples.go --pkg mocks --skip-ensure --with-resets . SamplesStore
//go:generate moq --out mocks/dictionary.go --pkg mocks --skip-ensure --with-resets . DictStore

// SpamFilter bot checks if a user is a spammer using lib.Detector
// Reloads spam samples, stop words and excluded tokens on file change.
type SpamFilter struct {
	Detector
	params SpamConfig
}

// SpamConfig is a full set of parameters for spam bot
type SpamConfig struct {
	SamplesStore SamplesStore // storage for spam samples
	DictStore    DictStore    // storage for stop words and excluded tokens

	SpamMsg    string
	SpamDryMsg string
	GroupID    string
	Dry        bool
}

// Detector is a spam detector interface
type Detector interface {
	Check(request spamcheck.Request) (spam bool, cr []spamcheck.Response)
	LoadSamples(exclReader io.Reader, spamReaders, hamReaders []io.Reader) (tgspam.LoadResult, error)
	LoadStopWords(readers ...io.Reader) (tgspam.LoadResult, error)
	UpdateSpam(msg string) error
	UpdateHam(msg string) error
	RemoveHam(msg string) error
	RemoveSpam(msg string) error
	AddApprovedUser(user approved.UserInfo) error
	RemoveApprovedUser(id string) error
	ApprovedUsers() (res []approved.UserInfo)
	IsApprovedUser(userID string) bool
	GetLuaPluginNames() []string // Returns the list of available Lua plugin names
}

// SamplesStore is a storage for spam samples
type SamplesStore interface {
	Read(ctx context.Context, t storage.SampleType, o storage.SampleOrigin) ([]string, error)
	Reader(ctx context.Context, t storage.SampleType, o storage.SampleOrigin) (io.ReadCloser, error)
	Stats(ctx context.Context) (*storage.SamplesStats, error)
}

// DictStore is a storage for dictionaries, i.e. stop words and ignored words
type DictStore interface {
	Reader(ctx context.Context, t storage.DictionaryType) (io.ReadCloser, error)
}

// NewSpamFilter creates new spam filter
func NewSpamFilter(detector Detector, params SpamConfig) *SpamFilter {
	return &SpamFilter{Detector: detector, params: params}
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
	if msg.WithAudio {
		spamReq.Meta.HasAudio = true
	}
	if msg.WithForward {
		spamReq.Meta.HasForward = true
	}
	if msg.WithKeyboard {
		spamReq.Meta.HasKeyboard = true
	}
	spamReq.Meta.Links = strings.Count(msg.Text, "http://") + strings.Count(msg.Text, "https://")

	// count mentions from entities
	if msg.Entities != nil {
		for _, entity := range *msg.Entities {
			if entity.Type == "mention" || entity.Type == "text_mention" {
				spamReq.Meta.Mentions++
			}
		}
	}
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
	log.Printf("[INFO] updated spam samples with %q", cleanMsg)
	return nil
}

// UpdateHam appends a message to the ham samples file and updates the classifier
func (s *SpamFilter) UpdateHam(msg string) error {
	cleanMsg := strings.ReplaceAll(msg, "\n", " ")
	log.Printf("[DEBUG] update ham samples with %q", cleanMsg)
	if err := s.Detector.UpdateHam(cleanMsg); err != nil {
		return fmt.Errorf("can't update ham samples: %w", err)
	}
	log.Printf("[INFO] updated ham samples with %q", cleanMsg)
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

// ReloadSamples reloads samples and stop-words
func (s *SpamFilter) ReloadSamples() (err error) {
	log.Printf("[DEBUG] reloading samples")

	var exclReader, spamReader, hamReader, stopWordsReader, spamDynamicReader, hamDynamicReader io.ReadCloser
	ctx := context.TODO()

	// check mandatory data presence
	st, err := s.params.SamplesStore.Stats(ctx)
	if err != nil {
		return fmt.Errorf("failed to get samples store stats: %w", err)
	}
	if st.PresetSpam == 0 || st.PresetHam == 0 {
		return fmt.Errorf("no pesistent spam or ham samples found in the store")
	}

	if spamReader, err = s.params.SamplesStore.Reader(ctx, storage.SampleTypeSpam, storage.SampleOriginPreset); err != nil {
		return fmt.Errorf("failed to get persistent spam samples: %w", err)
	}
	defer spamReader.Close()

	if hamReader, err = s.params.SamplesStore.Reader(ctx, storage.SampleTypeHam, storage.SampleOriginPreset); err != nil {
		return fmt.Errorf("failed to get persistent ham samples: %w", err)
	}
	defer hamReader.Close()

	if spamDynamicReader, err = s.params.SamplesStore.Reader(ctx, storage.SampleTypeSpam, storage.SampleOriginUser); err != nil {
		return fmt.Errorf("failed to get dynamic spam samples: %w", err)
	}
	defer spamDynamicReader.Close()

	if hamDynamicReader, err = s.params.SamplesStore.Reader(ctx, storage.SampleTypeHam, storage.SampleOriginUser); err != nil {
		return fmt.Errorf("failed to get dynamic ham samples: %w", err)
	}
	defer hamDynamicReader.Close()

	// stop-words are optional
	if stopWordsReader, err = s.params.DictStore.Reader(ctx, storage.DictionaryTypeStopPhrase); err != nil {
		return fmt.Errorf("failed to get stop words: %w", err)
	}
	defer stopWordsReader.Close()

	// excluded tokens are optional
	if exclReader, err = s.params.DictStore.Reader(ctx, storage.DictionaryTypeIgnoredWord); err != nil {
		return fmt.Errorf("failed to get excluded tokens: %w", err)
	}
	defer exclReader.Close()

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

	if spam, err = s.params.SamplesStore.Read(context.TODO(), storage.SampleTypeSpam, storage.SampleOriginUser); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to read dynamic spam samples: %w", err))
	}

	if ham, err = s.params.SamplesStore.Read(context.TODO(), storage.SampleTypeHam, storage.SampleOriginUser); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to read dynamic ham samples: %w", err))
	}

	if err := errs.ErrorOrNil(); err != nil {
		return spam, ham, fmt.Errorf("failed to read dynamic samples: %w", err)
	}
	return spam, ham, nil
}

// RemoveDynamicSpamSample removes a sample from the spam dynamic samples file and reloads samples after this
func (s *SpamFilter) RemoveDynamicSpamSample(sample string) error {
	cleanMsg := strings.ReplaceAll(sample, "\n", " ")
	log.Printf("[INFO] remove dynamic spam sample: %q", sample)
	if err := s.RemoveSpam(cleanMsg); err != nil {
		return fmt.Errorf("can't remove spam sample %q: %w", sample, err)
	}
	return nil
}

// RemoveDynamicHamSample removes a sample from the ham dynamic samples file and reloads samples after this
func (s *SpamFilter) RemoveDynamicHamSample(sample string) error {
	cleanMsg := strings.ReplaceAll(sample, "\n", " ")
	log.Printf("[INFO] remove dynamic ham sample: %q", sample)
	if err := s.RemoveHam(cleanMsg); err != nil {
		return fmt.Errorf("can't remove hma sample %q: %w", sample, err)
	}
	return nil
}
