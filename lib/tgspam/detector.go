package tgspam

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/forPelevin/gomoji"

	"github.com/umputun/tg-spam/lib/approved"
	"github.com/umputun/tg-spam/lib/spamcheck"
	"github.com/umputun/tg-spam/lib/tgspam/plugin"
)

//go:generate moq --out mocks/sample_updater.go --pkg mocks --skip-ensure --with-resets . SampleUpdater
//go:generate moq --out mocks/http_client.go --pkg mocks --skip-ensure --with-resets . HTTPClient
//go:generate moq --out mocks/user_storage.go --pkg mocks --skip-ensure --with-resets . UserStorage
//go:generate moq --out mocks/lua_plugin_engine.go --pkg mocks --skip-ensure --with-resets . LuaPluginEngine

// Detector is a spam detector, thread-safe.
// It uses a set of checks to determine if a message is spam, and also keeps a list of approved users.
type Detector struct {
	Config
	classifier     classifier
	openaiChecker  *openAIChecker
	metaChecks     []MetaCheck
	luaChecks      []plugin.Check // separate field for Lua plugin checks
	tokenizedSpam  []map[string]int
	approvedUsers  map[string]approved.UserInfo
	stopWords      []string
	excludedTokens map[string]struct{}
	luaEngine      LuaPluginEngine

	spamSamplesUpd SampleUpdater
	hamSamplesUpd  SampleUpdater
	userStorage    UserStorage

	// history of recent messages to keep in memory
	// can be passed to checkers supporting history
	hamHistory  *spamcheck.LastRequests
	spamHistory *spamcheck.LastRequests

	lock sync.RWMutex
}

// Config is a set of parameters for Detector.
type Config struct {
	SimilarityThreshold float64       // threshold for spam similarity, 0.0 - 1.0
	MinMsgLen           int           // minimum message length to check
	MaxAllowedEmoji     int           // maximum number of emojis allowed in a message
	CasAPI              string        // CAS API URL
	CasUserAgent        string        // CAS API User-Agent header value, set only if non-empty
	FirstMessageOnly    bool          // if true, only the first message from a user is checked
	FirstMessagesCount  int           // number of first messages to check for spam
	HTTPClient          HTTPClient    // http client to use for requests
	MinSpamProbability  float64       // minimum spam probability to consider a message spam with classifier, if 0 - ignored
	OpenAIVeto          bool          // if true, openai will be used to veto spam messages, otherwise it will be used to veto ham messages
	OpenAIHistorySize   int           // history size for openai
	MultiLangWords      int           // if true, check for number of multi-lingual words
	StorageTimeout      time.Duration // timeout for storage operations, if not set - no timeout

	LuaPlugins struct {
		Enabled        bool     // if true, enable Lua plugins
		PluginsDir     string   // directory with Lua plugins
		EnabledPlugins []string // list of enabled plugins (by name, without .lua extension)
		DynamicReload  bool     // if true, enable dynamic reloading of Lua plugins when files change
	}

	AbnormalSpacing struct {
		Enabled                 bool    // if true, enable check for abnormal spacing
		MinWordsCount           int     // the minimum number of words in the message to be considered
		ShortWordLen            int     // the length of the word to be considered short (in rune characters)
		ShortWordRatioThreshold float64 // the ratio of short words to all words in the message
		SpaceRatioThreshold     float64 // the ratio of spaces to all characters in the message
	}
	HistorySize int // history of recent messages to keep in memory
}

// SampleUpdater is an interface for updating spam/ham samples on the fly.
type SampleUpdater interface {
	Append(msg string) error        // append a message to the samples storage
	Remove(msg string) error        // remove a message from the samples storage
	Reader() (io.ReadCloser, error) // return a reader for the samples storage
}

// UserStorage is an interface for approved users storage.
type UserStorage interface {
	Read(ctx context.Context) ([]approved.UserInfo, error) // read approved users from storage
	Write(ctx context.Context, au approved.UserInfo) error // write approved user to storage
	Delete(ctx context.Context, id string) error           // delete approved user from storage
}

// HTTPClient is an interface for http client, satisfied by http.Client.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// LuaPluginEngine defines an interface for the Lua plugin system
type LuaPluginEngine interface {
	LoadScript(path string) error               // loads a single Lua script
	ReloadScript(path string) error             // reloads a single Lua script
	LoadDirectory(dir string) error             // loads all Lua scripts from a directory
	GetCheck(name string) (plugin.Check, error) // returns a specific named plugin check
	GetAllChecks() map[string]plugin.Check      // returns all loaded plugin checks
	Close()                                     // cleans up resources
}

// LoadResult is a result of loading samples.
type LoadResult struct {
	ExcludedTokens int // number of excluded tokens
	SpamSamples    int // number of spam samples
	HamSamples     int // number of ham samples
	StopWords      int // number of stop words (phrases)
}

// NewDetector makes a new Detector with the given config.
func NewDetector(p Config) *Detector {
	res := &Detector{
		Config:        p,
		classifier:    newClassifier(),
		approvedUsers: make(map[string]approved.UserInfo),
		tokenizedSpam: []map[string]int{},
		metaChecks:    []MetaCheck{},
		luaChecks:     []plugin.Check{},
		hamHistory:    spamcheck.NewLastRequests(p.HistorySize),
		spamHistory:   spamcheck.NewLastRequests(p.HistorySize),
		luaEngine:     nil, // will be set with WithLuaEngine if needed
	}
	// if FirstMessagesCount is set, FirstMessageOnly enforced to true.
	// this is to avoid confusion when FirstMessagesCount is set but FirstMessageOnly is false.
	// the reason for the redundant FirstMessageOnly flag is to avoid breaking api compatibility.
	if p.FirstMessagesCount > 0 {
		res.FirstMessageOnly = true
	}
	if p.FirstMessageOnly && p.FirstMessagesCount == 0 {
		res.FirstMessagesCount = 1 // default value for FirstMessagesCount if FirstMessageOnly is set
	}
	return res
}

// Check checks if a given message is spam. Returns true if spam and also returns a list of check results.
func (d *Detector) Check(req spamcheck.Request) (spam bool, cr []spamcheck.Response) {

	isSpamDetected := func(cr []spamcheck.Response) bool {
		for _, r := range cr {
			if r.Spam {
				return true
			}
		}
		return false
	}

	cleanMsg := d.cleanText(req.Msg)
	d.lock.RLock()
	defer d.lock.RUnlock()

	// approved user don't need to be checked
	if req.UserID != "" && d.FirstMessageOnly && d.approvedUsers[req.UserID].Count >= d.FirstMessagesCount {
		return false, []spamcheck.Response{{Name: "pre-approved", Spam: false, Details: "user already approved"}}
	}

	// all the checks are performed sequentially, so we can collect all the results

	// check for stop words if any stop words are loaded
	if len(d.stopWords) > 0 {
		cr = append(cr, d.isStopWord(cleanMsg, req))
	}

	// check for emojis if max allowed emojis is set
	if d.MaxAllowedEmoji >= 0 {
		cr = append(cr, d.isManyEmojis(req.Msg))
	}

	// check for spam with meta-checks
	for _, mc := range d.metaChecks {
		cr = append(cr, mc(req))
	}

	// check for spam with Lua plugin checks
	for _, lc := range d.luaChecks {
		cr = append(cr, lc(req))
	}

	// check for spam with CAS API if CAS API URL is set
	if d.CasAPI != "" {
		cr = append(cr, d.isCasSpam(req.UserID))
	}

	if d.MultiLangWords > 0 {
		cr = append(cr, d.isMultiLang(req.Msg))
	}

	if d.AbnormalSpacing.Enabled {
		cr = append(cr, d.isAbnormalSpacing(req.Msg))
	}

	// check for message length exceed the minimum size, if min message length is set.
	// the check is done after first simple checks, because stop words and emojis can be triggered by short messages as well.
	if len([]rune(req.Msg)) < d.MinMsgLen {
		cr = append(cr, spamcheck.Response{Name: "message length", Spam: false, Details: "too short"})
		if isSpamDetected(cr) {
			d.spamHistory.Push(req)
			return true, cr // spam from the checks above
		}
		d.hamHistory.Push(req)
		return false, cr
	}

	// check for spam similarity if a similarity threshold is set and spam samples are loaded
	if d.SimilarityThreshold > 0 && len(d.tokenizedSpam) > 0 {
		cr = append(cr, d.isSpamSimilarityHigh(cleanMsg))
	}

	// check for spam with classifier if classifier is loaded
	if d.classifier.nAllDocument > 0 && d.classifier.nDocumentByClass["ham"] > 0 && d.classifier.nDocumentByClass["spam"] > 0 {
		cr = append(cr, d.isSpamClassified(cleanMsg))
	}

	spamDetected := isSpamDetected(cr)

	// we hit openai in two cases:
	//  - all other checks passed (ham result) and OpenAIVeto is false. In this case, openai primary used to improve false negative rate
	//  - one of the checks failed (spam result) and OpenAIVeto is true. In this case, openai primary used to improve false positive rate
	// FirstMessageOnly or FirstMessagesCount has to be set to use openai, because it's slow and expensive to run on all messages
	if d.openaiChecker != nil && (d.FirstMessageOnly || d.FirstMessagesCount > 0) {
		if !spamDetected && !d.OpenAIVeto || spamDetected && d.OpenAIVeto {
			var hist []spamcheck.Request // by default, openai doesn't use history
			if d.OpenAIHistorySize > 0 && d.HistorySize > 0 {
				// if history size is set, we use the last N messages for openai
				hist = d.hamHistory.Last(d.OpenAIHistorySize)
			}
			spam, details := d.openaiChecker.check(cleanMsg, hist)
			cr = append(cr, details)
			if spamDetected && details.Error != nil {
				// spam detected with other checks, but openai failed. in this case, we still return spam, but log the error
				log.Printf("[WARN] openai error: %v", details.Error)
			} else {
				log.Printf("[DEBUG] openai result: {%s}", details.String())
				spamDetected = spam
			}

			// log if veto is enabled, and openai detected no spam for message that was detected as spam by other checks
			if d.OpenAIVeto && !spam {
				log.Printf("[DEBUG] openai vetoed ham message: %q, checks: %s", req.Msg, spamcheck.ChecksToString(cr))
			}
		}
	}

	if spamDetected {
		d.spamHistory.Push(req)
		return true, cr
	}

	// update approved users only if it's not paranoid mode and not a check-only request
	if (d.FirstMessageOnly || d.FirstMessagesCount > 0) && !req.CheckOnly {
		ctx, cancel := d.ctxWithStoreTimeout()
		defer cancel()
		au := approved.UserInfo{
			Count:     d.approvedUsers[req.UserID].Count + 1,
			UserID:    req.UserID,
			UserName:  req.UserName,
			Timestamp: time.Now(),
		}
		d.approvedUsers[req.UserID] = au // update approved users status in memory
		if d.userStorage != nil {
			// update approved users status in storage
			_ = d.userStorage.Write(ctx, au) // ignore error, failed to write to storage is not critical here
		}
	}
	d.hamHistory.Push(req)
	return false, cr
}

// Reset resets spam samples/classifier, excluded tokens, stop words and approved users.
func (d *Detector) Reset() {
	d.lock.Lock()
	defer d.lock.Unlock()

	d.tokenizedSpam = []map[string]int{}
	d.excludedTokens = map[string]struct{}{}
	d.classifier.reset()
	d.approvedUsers = make(map[string]approved.UserInfo)
	d.stopWords = []string{}

	// close the Lua engine and reset Lua checks if it exists
	if d.luaEngine != nil {
		d.luaEngine.Close()
		d.luaEngine = nil
		d.luaChecks = nil
	}
}

// WithOpenAIChecker sets an openAIChecker for spam checking.
func (d *Detector) WithOpenAIChecker(client openAIClient, config OpenAIConfig) {
	d.openaiChecker = newOpenAIChecker(client, config)
}

// WithLuaEngine sets a Lua plugin engine and loads plugins
func (d *Detector) WithLuaEngine(engine LuaPluginEngine) error {
	d.luaEngine = engine

	if !d.LuaPlugins.Enabled || d.LuaPlugins.PluginsDir == "" {
		return nil
	}

	// load all plugins from the directory
	if err := d.luaEngine.LoadDirectory(d.LuaPlugins.PluginsDir); err != nil {
		return fmt.Errorf("failed to load Lua plugins: %w", err)
	}

	// register enabled plugins as Lua checks
	if len(d.LuaPlugins.EnabledPlugins) > 0 {
		for _, name := range d.LuaPlugins.EnabledPlugins {
			pluginCheck, err := d.luaEngine.GetCheck(name)
			if err != nil {
				return fmt.Errorf("failed to get Lua check %q: %w", name, err)
			}
			// add to luaChecks
			d.luaChecks = append(d.luaChecks, pluginCheck)
		}
	} else {
		// if no specific plugins are enabled, load all
		allChecks := d.luaEngine.GetAllChecks()
		for _, pluginCheck := range allChecks {
			// add to luaChecks
			d.luaChecks = append(d.luaChecks, pluginCheck)
		}
	}

	// set up a watcher for dynamic plugin reloading if enabled
	if d.LuaPlugins.DynamicReload {
		// we need to cast the luaEngine to a *plugin.Checker to access the watcher methods
		checker, ok := d.luaEngine.(*plugin.Checker)
		if !ok {
			log.Printf("[WARN] dynamic Lua plugin reloading enabled but engine doesn't support it")
			return nil
		}

		// create a watcher for the plugins directory
		watcher, err := plugin.NewWatcher(checker, d.LuaPlugins.PluginsDir)
		if err != nil {
			return fmt.Errorf("failed to create watcher for Lua plugins: %w", err)
		}

		// set the watcher on the checker
		checker.SetWatcher(watcher)

		// start the watcher
		if err := watcher.Start(); err != nil {
			return fmt.Errorf("failed to start watcher for Lua plugins: %w", err)
		}
	}

	return nil
}

// WithUserStorage sets a UserStorage for approved users and loads approved users from it.
func (d *Detector) WithUserStorage(storage UserStorage) (count int, err error) {
	d.lock.Lock()
	defer d.lock.Unlock()
	d.approvedUsers = make(map[string]approved.UserInfo) // reset approved users
	d.userStorage = storage

	ctx, cancel := d.ctxWithStoreTimeout()
	defer cancel()

	users, err := d.userStorage.Read(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to read approved users from storage: %w", err)
	}
	for _, user := range users {
		user.Count = d.FirstMessagesCount + 1 // +1 to skip first message check if count is 0
		d.approvedUsers[user.UserID] = user
	}
	return len(users), nil
}

// WithMetaChecks sets a list of meta-checkers.
func (d *Detector) WithMetaChecks(mc ...MetaCheck) {
	d.metaChecks = append(d.metaChecks, mc...)
}

// WithSpamUpdater sets a SampleUpdater for spam samples.
func (d *Detector) WithSpamUpdater(s SampleUpdater) { d.spamSamplesUpd = s }

// WithHamUpdater sets a SampleUpdater for ham samples.
func (d *Detector) WithHamUpdater(s SampleUpdater) { d.hamSamplesUpd = s }

// ApprovedUsers returns a list of approved users.
func (d *Detector) ApprovedUsers() (res []approved.UserInfo) {
	d.lock.RLock()
	defer d.lock.RUnlock()
	res = make([]approved.UserInfo, 0, len(d.approvedUsers))
	for _, info := range d.approvedUsers {
		res = append(res, info)
	}
	sort.Slice(res, func(i, j int) bool {
		return res[i].Timestamp.After(res[j].Timestamp)
	})
	return res
}

// IsApprovedUser checks if a given user ID is approved.
// It uses memory cache for approved users and compares the count of messages sent by the user.
func (d *Detector) IsApprovedUser(userID string) bool {
	d.lock.RLock()
	defer d.lock.RUnlock()

	ui, ok := d.approvedUsers[userID]
	if !ok {
		return false
	}
	return ui.Count > d.FirstMessagesCount
}

// AddApprovedUser adds user IDs to the list of approved users.
func (d *Detector) AddApprovedUser(user approved.UserInfo) error {
	d.lock.Lock()
	defer d.lock.Unlock()
	ts := user.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	d.approvedUsers[user.UserID] = approved.UserInfo{
		UserID:    user.UserID,
		UserName:  user.UserName,
		Count:     d.FirstMessagesCount + 1, // +1 to skip first message check if count is 0
		Timestamp: ts,
	}

	if d.userStorage != nil {
		ctx, cancel := d.ctxWithStoreTimeout()
		defer cancel()
		if err := d.userStorage.Write(ctx, user); err != nil {
			return fmt.Errorf("failed to write approved user %+v to storage: %w", user, err)
		}
	}
	return nil
}

// RemoveApprovedUser removes approved user for given IDs
func (d *Detector) RemoveApprovedUser(id string) error {
	d.lock.Lock()
	delete(d.approvedUsers, id)
	d.lock.Unlock()

	if d.userStorage != nil {
		ctx, cancel := d.ctxWithStoreTimeout()
		defer cancel()
		if err := d.userStorage.Delete(ctx, id); err != nil {
			return fmt.Errorf("failed to delete approved user %s from storage: %w", id, err)
		}
	}
	return nil
}

// GetLuaPluginNames returns the list of available Lua plugin names.
func (d *Detector) GetLuaPluginNames() []string {
	d.lock.RLock()
	defer d.lock.RUnlock()

	if d.luaEngine == nil || !d.LuaPlugins.Enabled {
		return []string{}
	}

	allChecks := d.luaEngine.GetAllChecks()
	result := make([]string, 0, len(allChecks))

	for name := range allChecks {
		result = append(result, name)
	}

	// sort the result for consistent output
	sort.Strings(result)
	return result
}

// LoadSamples loads spam samples from a reader and updates the classifier.
// Reset spam, ham samples/classifier, and excluded tokens.
func (d *Detector) LoadSamples(exclReader io.Reader, spamReaders, hamReaders []io.Reader) (LoadResult, error) {
	d.lock.Lock()
	defer d.lock.Unlock()

	d.tokenizedSpam = []map[string]int{}
	d.excludedTokens = map[string]struct{}{}
	d.classifier.reset()

	// excluded tokens should be loaded before spam samples to exclude them from spam tokenization
	for t := range d.readerIterator(exclReader) {
		d.excludedTokens[strings.ToLower(t)] = struct{}{}
	}
	lr := LoadResult{ExcludedTokens: len(d.excludedTokens)}

	// load spam samples and update the classifier with them
	docs := []document{}
	for token := range d.readerIterator(spamReaders...) {
		tokenizedSpam := d.tokenize(token)
		d.tokenizedSpam = append(d.tokenizedSpam, tokenizedSpam) // add to list of samples
		tokens := make([]string, 0, len(tokenizedSpam))
		for token := range tokenizedSpam {
			tokens = append(tokens, token)
		}
		docs = append(docs, newDocument(ClassSpam, tokens...))
		lr.SpamSamples++
	}

	// load ham samples and update the classifier with them
	for token := range d.readerIterator(hamReaders...) {
		tokenizedSpam := d.tokenize(token)
		tokens := make([]string, 0, len(tokenizedSpam))
		for token := range tokenizedSpam {
			tokens = append(tokens, token)
		}
		docs = append(docs, document{spamClass: ClassHam, tokens: tokens})
		lr.HamSamples++
	}

	d.classifier.learn(docs...)
	return lr, nil
}

// LoadStopWords loads stop words from a reader. Reset stop words list before loading.
func (d *Detector) LoadStopWords(readers ...io.Reader) (LoadResult, error) {
	d.lock.Lock()
	defer d.lock.Unlock()

	d.stopWords = []string{}
	for t := range d.readerIterator(readers...) {
		d.stopWords = append(d.stopWords, strings.ToLower(t))
	}
	return LoadResult{StopWords: len(d.stopWords)}, nil
}

// UpdateSpam appends a message to the spam samples file and updates the classifier
func (d *Detector) UpdateSpam(msg string) error {
	return d.updateSample(msg, d.spamSamplesUpd, ClassSpam)
}

// UpdateHam appends a message to the ham samples file and updates the classifier
func (d *Detector) UpdateHam(msg string) error {
	return d.updateSample(msg, d.hamSamplesUpd, ClassHam)
}

// RemoveSpam removes a message from the spam samples file and updates the classifier by unlearning
func (d *Detector) RemoveSpam(msg string) error {
	return d.removeSample(msg, d.spamSamplesUpd, ClassSpam)
}

// RemoveHam removes a message from the ham samples file and updates the classifier by unlearning
func (d *Detector) RemoveHam(msg string) error {
	return d.removeSample(msg, d.hamSamplesUpd, ClassHam)
}

// updateSample appends a message to the samples store and updates the classifier
// doesn't reset state, update append samples
func (d *Detector) updateSample(msg string, upd SampleUpdater, sc spamClass) error {
	d.lock.Lock()
	defer d.lock.Unlock()

	if upd == nil {
		return nil
	}

	// write to dynamic samples storage
	if err := upd.Append(msg); err != nil {
		return fmt.Errorf("can't update %s samples: %w", sc, err)
	}

	// load samples and update the classifier with them
	docs := d.buildDocs(msg, sc)
	d.classifier.learn(docs...)

	// update tokenized spam samples for similarity check
	if sc == ClassSpam {
		tokenizedSpam := d.tokenize(msg)
		d.tokenizedSpam = append(d.tokenizedSpam, tokenizedSpam)
	}

	return nil
}

// removeSample removes a message from the spam samples file and updates the classifier by unlearning
func (d *Detector) removeSample(msg string, upd SampleUpdater, sc spamClass) error {
	d.lock.Lock()
	defer d.lock.Unlock()

	if upd == nil {
		return nil
	}

	// first validate that we can unlearn this sample
	docs := d.buildDocs(msg, sc)
	if err := d.classifier.unlearn(docs...); err != nil {
		return fmt.Errorf("can't unlearn %s samples: %w", sc, err)
	}

	// if unlearn succeeded, remove from storage
	if err := upd.Remove(msg); err != nil {
		// try to relearn since storage update failed
		d.classifier.learn(docs...)
		return fmt.Errorf("can't remove %s samples: %w", sc, err)
	}
	return nil
}

// buildDocs builds a list of classifier documents from a message
func (d *Detector) buildDocs(msg string, sc spamClass) []document {
	docs := []document{}
	for token := range d.readerIterator(bytes.NewBufferString(msg)) {
		tokenizedSample := d.tokenize(token)
		tokens := make([]string, 0, len(tokenizedSample))
		for token := range tokenizedSample {
			tokens = append(tokens, token)
		}
		docs = append(docs, document{spamClass: sc, tokens: tokens})
	}
	return docs
}

// readerIterator parses readers and returns an iterator of data elements, each line is an element.
func (d *Detector) readerIterator(readers ...io.Reader) iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, reader := range readers {
			scanner := bufio.NewScanner(reader)
			for scanner.Scan() {
				line := scanner.Text()
				// each line with a single element
				cleanToken := strings.Trim(line, " \n\r\t")
				if cleanToken != "" {
					if !yield(cleanToken) {
						return
					}
				}
			}

			if err := scanner.Err(); err != nil {
				log.Printf("[WARN] failed to read tokens, error=%v", err)
			}
		}
	}
}

// tokenize takes a string and returns a map where the keys are unique words (tokens)
// and the values are the frequencies of those words in the string.
// exclude tokens representing common words.
func (d *Detector) tokenize(inp string) map[string]int {
	isExcludedToken := func(token string) bool {
		if _, ok := d.excludedTokens[strings.ToLower(token)]; ok {
			return true
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

// isSpam checks if a given message is similar to any of the known bad messages
func (d *Detector) isSpamSimilarityHigh(msg string) spamcheck.Response {
	// check for spam similarity
	tokenizedMessage := d.tokenize(msg)
	maxSimilarity := 0.0
	for _, spam := range d.tokenizedSpam {
		similarity := d.cosineSimilarity(tokenizedMessage, spam)
		if similarity > maxSimilarity {
			maxSimilarity = similarity
		}
		if similarity >= d.SimilarityThreshold {
			return spamcheck.Response{Spam: true, Name: "similarity",
				Details: fmt.Sprintf("%0.2f/%0.2f", maxSimilarity, d.SimilarityThreshold)}
		}
	}
	return spamcheck.Response{Spam: false, Name: "similarity", Details: fmt.Sprintf("%0.2f/%0.2f", maxSimilarity, d.SimilarityThreshold)}
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

// isCasSpam checks if a given user ID is a spammer with CAS API.
func (d *Detector) isCasSpam(msgID string) spamcheck.Response {
	if msgID == "" {
		return spamcheck.Response{Spam: false, Name: "cas", Details: "check disabled"}
	}
	if _, err := strconv.ParseInt(msgID, 10, 64); err != nil {
		return spamcheck.Response{Spam: false, Name: "cas", Details: fmt.Sprintf("invalid user id %q", msgID)}
	}
	reqURL := fmt.Sprintf("%s/check?user_id=%s", d.CasAPI, msgID)
	req, err := http.NewRequest("GET", reqURL, http.NoBody)
	if err != nil {
		return spamcheck.Response{Spam: false, Name: "cas", Details: fmt.Sprintf("failed to make request %s: %v", reqURL, err)}
	}

	if d.CasUserAgent != "" {
		req.Header.Set("User-Agent", d.CasUserAgent)
	}

	resp, err := d.HTTPClient.Do(req)
	if err != nil {
		return spamcheck.Response{Spam: false, Name: "cas", Details: fmt.Sprintf("ffailed to send request %s: %v", reqURL, err)}
	}
	defer resp.Body.Close()

	respData := struct {
		OK          bool   `json:"ok"` // ok means user is a spammer
		Description string `json:"description"`
	}{}

	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		return spamcheck.Response{Spam: false, Name: "cas", Details: fmt.Sprintf("failed to parse response from %s: %v", reqURL, err)}
	}
	respData.Description = strings.ToLower(respData.Description)
	respData.Description = strings.TrimSuffix(respData.Description, ".")

	if respData.OK {
		// may return empty description on detected spam
		if respData.Description == "" {
			respData.Description = "spam detected"
		}
		return spamcheck.Response{Name: "cas", Spam: true, Details: respData.Description}
	}
	details := respData.Description
	if details == "" {
		details = "not found"
	}
	return spamcheck.Response{Name: "cas", Spam: false, Details: details}
}

// isSpamClassified classify tokens from a document
func (d *Detector) isSpamClassified(msg string) spamcheck.Response {
	tm := d.tokenize(msg)
	tokens := make([]string, 0, len(tm))
	for token := range tm {
		tokens = append(tokens, token)
	}
	class, prob, certain := d.classifier.classify(tokens...)
	isSpam := class == ClassSpam && certain && (d.MinSpamProbability == 0 || prob >= d.MinSpamProbability)
	return spamcheck.Response{Name: "classifier", Spam: isSpam,
		Details: fmt.Sprintf("probability of %s: %.2f%%", class, prob)}
}

// isStopWord checks if a given message or username contains any of the stop words.
func (d *Detector) isStopWord(msg string, req spamcheck.Request) spamcheck.Response {
	// check message text
	cleanMsg := cleanEmoji(strings.ToLower(msg))
	for _, word := range d.stopWords { // stop words are already lowercased
		if strings.Contains(cleanMsg, strings.ToLower(word)) {
			return spamcheck.Response{Name: "stopword", Spam: true, Details: word}
		}
	}

	// check username and user id if they are not empty for stop words
	names := []string{}
	if req.UserName != "" {
		names = append(names, req.UserName)
	}
	if req.UserID != "" {
		names = append(names, req.UserID)
	}
	for _, name := range names {
		for _, word := range d.stopWords {
			if strings.Contains(strings.ToLower(name), strings.ToLower(word)) {
				return spamcheck.Response{Name: "stopword", Spam: true, Details: word}
			}
		}
	}

	return spamcheck.Response{Name: "stopword", Spam: false, Details: "not found"}
}

// isManyEmojis checks if a given message contains more than MaxAllowedEmoji emojis.
func (d *Detector) isManyEmojis(msg string) spamcheck.Response {
	count := countEmoji(msg)
	return spamcheck.Response{Name: "emoji", Spam: count > d.MaxAllowedEmoji, Details: fmt.Sprintf("%d/%d", count, d.MaxAllowedEmoji)}
}

// isMultiLang checks if a given message contains more than MultiLangWords multi-lingual words.
func (d *Detector) isMultiLang(msg string) spamcheck.Response {
	isMultiLingual := func(word string) bool {
		scripts := make(map[string]bool)
		for _, r := range word {
			if r == 'i' || unicode.IsSpace(r) || unicode.IsNumber(r) { // skip 'i' (common in many langs) and spaces
				continue
			}

			scriptFound := false
			for name, table := range unicode.Scripts {
				if unicode.Is(table, r) {
					if name != "Common" && name != "Inherited" {
						scripts[name] = true
						if len(scripts) > 1 {
							return true
						}
						scriptFound = true
					}
					break
				}
			}

			// if no specific script was found, it might be a symbol or punctuation
			if !scriptFound {
				// check for mathematical alphanumeric symbols and letterlike symbols
				if unicode.In(r, unicode.Other_Math, unicode.Other_Alphabetic) ||
					(r >= '\U0001D400' && r <= '\U0001D7FF') || // mathematical Alphanumeric Symbols
					(r >= '\u2100' && r <= '\u214F') { // letterlike Symbols
					scripts["Mathematical"] = true
					if len(scripts) > 1 {
						return true
					}
				} else if !unicode.IsPunct(r) && !unicode.IsSymbol(r) {
					// if it's not punctuation or a symbol, count it as "Other"
					scripts["Other"] = true
					if len(scripts) > 1 {
						return true
					}
				}
			}
		}
		return false
	}

	count := 0
	words := strings.FieldsFunc(msg, func(r rune) bool {
		return unicode.IsSpace(r) || r == '-'
	})
	for _, word := range words {
		if isMultiLingual(word) {
			count++
		}
	}
	if count >= d.MultiLangWords {
		return spamcheck.Response{Name: "multi-lingual", Spam: true, Details: fmt.Sprintf("%d/%d", count, d.MultiLangWords)}
	}
	return spamcheck.Response{Name: "multi-lingual", Spam: false, Details: fmt.Sprintf("%d/%d", count, d.MultiLangWords)}
}

// isAbnormalSpacing detects abnormal spacing patterns used to evade filters
// things like this: "w o r d s p a c i n g some thing he re blah blah"
func (d *Detector) isAbnormalSpacing(msg string) spamcheck.Response {
	text := strings.ToUpper(msg)

	// quick check for empty or very short text
	if len(text) < 10 {
		return spamcheck.Response{
			Name:    "word-spacing",
			Spam:    false,
			Details: "too short",
		}
	}

	words := strings.Fields(text)
	// check for minimum number of words
	if len(words) < d.AbnormalSpacing.MinWordsCount {
		return spamcheck.Response{
			Name:    "word-spacing",
			Spam:    false,
			Details: fmt.Sprintf("too few words (%d)", len(words)),
		}
	}

	// count letters and spaces in original text
	var totalChars, spaces int
	for _, r := range text {
		if unicode.IsLetter(r) {
			totalChars++
		} else if unicode.IsSpace(r) {
			spaces++
		}
	}

	// look for suspicious word lengths and spacing patterns
	shortWords := 0
	if d.AbnormalSpacing.ShortWordLen > 0 { // if ShortWordLen is 0, skip short word detection
		for _, word := range words {
			wordRunes := []rune(word)
			if len(wordRunes) <= d.AbnormalSpacing.ShortWordLen && len(wordRunes) > 0 {
				shortWords++
			}
		}
	}

	// safety check
	if spaces == 0 || totalChars == 0 {
		return spamcheck.Response{
			Name:    "word-spacing",
			Spam:    false,
			Details: "no spaces or letters",
		}
	}

	// calculate ratios
	spaceRatio := float64(spaces) / float64(totalChars)
	shortWordRatio := float64(shortWords) / float64(len(words))
	if shortWordRatio > d.AbnormalSpacing.ShortWordRatioThreshold || spaceRatio > d.AbnormalSpacing.SpaceRatioThreshold {
		return spamcheck.Response{
			Name:    "word-spacing",
			Spam:    true,
			Details: fmt.Sprintf("abnormal (ratio: %.2f, short: %.0f%%)", spaceRatio, shortWordRatio*100),
		}
	}

	return spamcheck.Response{
		Name:    "word-spacing",
		Spam:    false,
		Details: fmt.Sprintf("normal (ratio: %.2f, short: %.0f%%)", spaceRatio, shortWordRatio*100),
	}
}

// cleanText removes control and format characters from a given text
func (d *Detector) cleanText(text string) string {
	var result strings.Builder
	result.Grow(len(text))
	for _, r := range text {
		// skip control and format characters
		if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		// skip specific ranges of invisible characters
		if (r >= 0x200B && r <= 0x200F) || (r >= 0x2060 && r <= 0x206F) {
			continue
		}
		result.WriteRune(r)
	}
	return result.String()
}

func (d *Detector) ctxWithStoreTimeout() (context.Context, context.CancelFunc) {
	if d.StorageTimeout == 0 {
		return context.Background(), func() {}
	}
	return context.WithTimeout(context.Background(), d.StorageTimeout)
}

func cleanEmoji(s string) string {
	return gomoji.RemoveEmojis(s)
}

func countEmoji(s string) int {
	return len(gomoji.CollectAll(s))
}
