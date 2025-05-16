package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	tbapi "github.com/OvyFlash/telegram-bot-api"
	"github.com/fatih/color"
	"github.com/go-pkgz/fileutils"
	"github.com/go-pkgz/lgr"
	"github.com/go-pkgz/rest"
	"github.com/jessevdk/go-flags"
	"github.com/sashabaranov/go-openai"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/umputun/tg-spam/app/bot"
	"github.com/umputun/tg-spam/app/events"
	"github.com/umputun/tg-spam/app/storage"
	"github.com/umputun/tg-spam/app/storage/engine"
	"github.com/umputun/tg-spam/app/webapi"
	"github.com/umputun/tg-spam/lib/tgspam"
	"github.com/umputun/tg-spam/lib/tgspam/plugin"
)

type options struct {
	InstanceID  string `long:"instance-id" env:"INSTANCE_ID" default:"tg-spam" description:"instance id"`
	DataBaseURL string `long:"db" env:"DB" default:"tg-spam.db" description:"database URL, if empty uses sqlite"`

	Telegram struct {
		Token        string        `long:"token" env:"TOKEN" description:"telegram bot token"`
		Group        string        `long:"group" env:"GROUP" description:"group name/id"`
		Timeout      time.Duration `long:"timeout" env:"TIMEOUT" default:"30s" description:"http client timeout for telegram" `
		IdleDuration time.Duration `long:"idle" env:"IDLE" default:"30s" description:"idle duration"`
	} `group:"telegram" namespace:"telegram" env-namespace:"TELEGRAM"`

	AdminGroup              string `long:"admin.group" env:"ADMIN_GROUP" description:"admin group name, or channel id"`
	DisableAdminSpamForward bool   `long:"disable-admin-spam-forward" env:"DISABLE_ADMIN_SPAM_FORWARD" description:"disable handling messages forwarded to admin group as spam"`

	TestingIDs []int64 `long:"testing-id" env:"TESTING_ID" env-delim:"," description:"testing ids, allow bot to reply to them"`

	HistoryDuration time.Duration `long:"history-duration" env:"HISTORY_DURATION" default:"24h" description:"history duration"`
	HistoryMinSize  int           `long:"history-min-size" env:"HISTORY_MIN_SIZE" default:"1000" description:"history minimal size to keep"`
	StorageTimeout  time.Duration `long:"storage-timeout" env:"STORAGE_TIMEOUT" default:"0s" description:"storage timeout"`

	Logger struct {
		Enabled    bool   `long:"enabled" env:"ENABLED" description:"enable spam rotated logs"`
		FileName   string `long:"file" env:"FILE"  default:"tg-spam.log" description:"location of spam log"`
		MaxSize    string `long:"max-size" env:"MAX_SIZE" default:"100M" description:"maximum size before it gets rotated"`
		MaxBackups int    `long:"max-backups" env:"MAX_BACKUPS" default:"10" description:"maximum number of old log files to retain"`
	} `group:"logger" namespace:"logger" env-namespace:"LOGGER"`

	SuperUsers          events.SuperUsers `long:"super" env:"SUPER_USER" env-delim:"," description:"super-users"`
	NoSpamReply         bool              `long:"no-spam-reply" env:"NO_SPAM_REPLY" description:"do not reply to spam messages"`
	SuppressJoinMessage bool              `long:"suppress-join-message" env:"SUPPRESS_JOIN_MESSAGE" description:"delete join message if user is kicked out"`

	CAS struct {
		API       string        `long:"api" env:"API" default:"https://api.cas.chat" description:"CAS API"`
		Timeout   time.Duration `long:"timeout" env:"TIMEOUT" default:"5s" description:"CAS timeout"`
		UserAgent string        `long:"user-agent" env:"USER_AGENT" description:"User-Agent header for CAS API requests"`
	} `group:"cas" namespace:"cas" env-namespace:"CAS"`

	Meta struct {
		LinksLimit      int    `long:"links-limit" env:"LINKS_LIMIT" default:"-1" description:"max links in message, disabled by default"`
		MentionsLimit   int    `long:"mentions-limit" env:"MENTIONS_LIMIT" default:"-1" description:"max mentions in message, disabled by default"`
		ImageOnly       bool   `long:"image-only" env:"IMAGE_ONLY" description:"enable image only check"`
		LinksOnly       bool   `long:"links-only" env:"LINKS_ONLY" description:"enable links only check"`
		VideosOnly      bool   `long:"video-only" env:"VIDEO_ONLY" description:"enable video only check"`
		AudiosOnly      bool   `long:"audio-only" env:"AUDIO_ONLY" description:"enable audio only check"`
		Forward         bool   `long:"forward" env:"FORWARD" description:"enable forward check"`
		Keyboard        bool   `long:"keyboard" env:"KEYBOARD" description:"enable keyboard check"`
		UsernameSymbols string `long:"username-symbols" env:"USERNAME_SYMBOLS" description:"prohibited symbols in username, disabled by default"`
	} `group:"meta" namespace:"meta" env-namespace:"META"`

	OpenAI struct {
		Token             string   `long:"token" env:"TOKEN" description:"openai token, disabled if not set"`
		APIBase           string   `long:"apibase" env:"API_BASE" description:"custom openai API base, default is https://api.openai.com/v1"`
		Veto              bool     `long:"veto" env:"VETO" description:"veto mode, confirm detected spam"`
		Prompt            string   `long:"prompt" env:"PROMPT" default:"" description:"openai system prompt, if empty uses builtin default"`
		CustomPrompts     []string `long:"custom-prompt" env:"CUSTOM_PROMPT" env-delim:"," description:"additional custom prompts for specific spam patterns"`
		Model             string   `long:"model" env:"MODEL" default:"gpt-4o-mini" description:"openai model"`
		MaxTokensResponse int      `long:"max-tokens-response" env:"MAX_TOKENS_RESPONSE" default:"1024" description:"openai max tokens in response"`
		MaxTokensRequest  int      `long:"max-tokens-request" env:"MAX_TOKENS_REQUEST" default:"2048" description:"openai max tokens in request"`
		MaxSymbolsRequest int      `long:"max-symbols-request" env:"MAX_SYMBOLS_REQUEST" default:"16000" description:"openai max symbols in request, failback if tokenizer failed"`
		RetryCount        int      `long:"retry-count" env:"RETRY_COUNT" default:"1" description:"openai retry count"`
		HistorySize       int      `long:"history-size" env:"HISTORY_SIZE" default:"0" description:"openai history size"`
		ReasoningEffort   string   `long:"reasoning-effort" env:"REASONING_EFFORT" default:"none" choice:"none" choice:"low" choice:"medium" choice:"high" description:"reasoning effort for thinking models, none disables thinking"`
	} `group:"openai" namespace:"openai" env-namespace:"OPENAI"`

	LuaPlugins struct {
		Enabled        bool     `long:"enabled" env:"ENABLED" description:"enable Lua plugins"`
		PluginsDir     string   `long:"plugins-dir" env:"PLUGINS_DIR" description:"directory with Lua plugins"`
		EnabledPlugins []string `long:"enabled-plugins" env:"ENABLED_PLUGINS" env-delim:"," description:"list of enabled plugins (by name, without .lua extension)"`
		DynamicReload  bool     `long:"dynamic-reload" env:"DYNAMIC_RELOAD" description:"dynamically reload plugins when they change"`
	} `group:"lua-plugins" namespace:"lua-plugins" env-namespace:"LUA_PLUGINS"`

	AbnormalSpacing struct {
		Enabled                 bool    `long:"enabled" env:"ENABLED" description:"enable abnormal words check"`
		SpaceRatioThreshold     float64 `long:"ratio" env:"RATIO" default:"0.3" description:"the ratio of spaces to all characters in the message"`
		ShortWordRatioThreshold float64 `long:"short-ratio" env:"SHORT_RATIO" default:"0.7" description:"the ratio of short words to all words in the message"`
		ShortWordLen            int     `long:"short-word" env:"SHORT_WORD" default:"3" description:"the length of the word to be considered short"`
		MinWords                int     `long:"min-words" env:"MIN_WORDS" default:"5" description:"the minimum number of words in the message to check"`
	} `group:"space" namespace:"space" env-namespace:"SPACE"`

	Files struct {
		SamplesDataPath string        `long:"samples" env:"SAMPLES" default:"preset" description:"samples data path, deprecated"`
		DynamicDataPath string        `long:"dynamic" env:"DYNAMIC" default:"data" description:"dynamic data path"`
		WatchInterval   time.Duration `long:"watch-interval" env:"WATCH_INTERVAL" default:"5s" description:"watch interval for dynamic files, deprecated"`
	} `group:"files" namespace:"files" env-namespace:"FILES"`

	SimilarityThreshold float64 `long:"similarity-threshold" env:"SIMILARITY_THRESHOLD" default:"0.5" description:"spam threshold"`
	MinMsgLen           int     `long:"min-msg-len" env:"MIN_MSG_LEN" default:"50" description:"min message length to check"`
	MaxEmoji            int     `long:"max-emoji" env:"MAX_EMOJI" default:"2" description:"max emoji count in message, -1 to disable check"`
	MinSpamProbability  float64 `long:"min-probability" env:"MIN_PROBABILITY" default:"50" description:"min spam probability percent to ban"`
	MultiLangWords      int     `long:"multi-lang" env:"MULTI_LANG" default:"0" description:"number of words in different languages to consider as spam"`

	ParanoidMode       bool `long:"paranoid" env:"PARANOID" description:"paranoid mode, check all messages"`
	FirstMessagesCount int  `long:"first-messages-count" env:"FIRST_MESSAGES_COUNT" default:"1" description:"number of first messages to check"`

	Message struct {
		Startup string `long:"startup" env:"STARTUP" default:"" description:"startup message"`
		Spam    string `long:"spam" env:"SPAM" default:"this is spam" description:"spam message"`
		Dry     string `long:"dry" env:"DRY" default:"this is spam (dry mode)" description:"spam dry message"`
		Warn    string `long:"warn" env:"WARN" default:"You've violated our rules and this is your first and last warning. Further violations will lead to permanent access denial. Stay compliant or face the consequences!" description:"warning message"`
	} `group:"message" namespace:"message" env-namespace:"MESSAGE"`

	Server struct {
		Enabled    bool   `long:"enabled" env:"ENABLED" description:"enable web server"`
		ListenAddr string `long:"listen" env:"LISTEN" default:":8080" description:"listen address"`
		AuthPasswd string `long:"auth" env:"AUTH" default:"auto" description:"basic auth password for user 'tg-spam'"`
		AuthHash   string `long:"auth-hash" env:"AUTH_HASH" default:"" description:"basic auth password hash for user 'tg-spam'"`
	} `group:"server" namespace:"server" env-namespace:"SERVER"`

	Training bool `long:"training" env:"TRAINING" description:"training mode, passive spam detection only"`
	SoftBan  bool `long:"soft-ban" env:"SOFT_BAN" description:"soft ban mode, restrict user actions but not ban"`

	HistorySize int    `long:"history-size" env:"LAST_MSGS_HISTORY_SIZE" default:"100" description:"history size"`
	Convert     string `long:"convert" choice:"only" choice:"enabled" choice:"disabled" default:"enabled" description:"convert mode for txt samples and other storage files to DB"`

	MaxBackups int `long:"max-backups" env:"MAX_BACKUPS" default:"10" description:"maximum number of backups to keep, set 0 to disable"`

	Dry   bool `long:"dry" env:"DRY" description:"dry mode, no bans"`
	Dbg   bool `long:"dbg" env:"DEBUG" description:"debug mode"`
	TGDbg bool `long:"tg-dbg" env:"TG_DEBUG" description:"telegram debug mode"`
}

// default file names
const (
	samplesSpamFile   = "spam-samples.txt"
	samplesHamFile    = "ham-samples.txt"
	excludeTokensFile = "exclude-tokens.txt" //nolint:gosec // false positive
	stopWordsFile     = "stop-words.txt"     //nolint:gosec // false positive
	dynamicSpamFile   = "spam-dynamic.txt"
	dynamicHamFile    = "ham-dynamic.txt"
	dataFile          = "tg-spam.db"
)

var revision = "local"

func main() {
	fmt.Printf("tg-spam %s\n", revision)
	var opts options
	p := flags.NewParser(&opts, flags.PrintErrors|flags.PassDoubleDash|flags.HelpFlag)
	p.SubcommandsOptional = true
	if _, err := p.Parse(); err != nil {
		if !errors.Is(err.(*flags.Error).Type, flags.ErrHelp) {
			log.Printf("[ERROR] cli error: %v", err)
			os.Exit(1)
		}
		os.Exit(2)
	}

	masked := []string{opts.Telegram.Token, opts.OpenAI.Token}
	if opts.Server.AuthPasswd != "auto" && opts.Server.AuthPasswd != "" {
		// auto passwd should not be masked as we print it
		masked = append(masked, opts.Server.AuthPasswd)
	}
	if opts.Server.AuthHash != "" {
		masked = append(masked, opts.Server.AuthHash)
	}

	setupLog(opts.Dbg, masked...)

	log.Printf("[DEBUG] options: %+v", opts)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		// catch signal and invoke graceful termination
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		<-stop
		log.Printf("[WARN] interrupt signal")
		cancel()
	}()

	// expand, make absolute paths
	opts.Files.DynamicDataPath = expandPath(opts.Files.DynamicDataPath)
	opts.Files.SamplesDataPath = expandPath(opts.Files.SamplesDataPath)

	if err := execute(ctx, opts); err != nil {
		log.Printf("[ERROR] %v", err)
		os.Exit(1)
	}
}

func execute(ctx context.Context, opts options) error {
	if opts.Dry {
		log.Print("[WARN] dry mode, no actual bans")
	}

	convertOnly := opts.Convert == "only"
	if !opts.Server.Enabled && !convertOnly && (opts.Telegram.Token == "" || opts.Telegram.Group == "") {
		return errors.New("telegram token and group are required")
	}

	checkVolumeMount(opts) // show warning if dynamic files dir not mounted

	// make samples and dynamic data dirs
	if err := os.MkdirAll(opts.Files.SamplesDataPath, 0o700); err != nil {
		return fmt.Errorf("can't make samples dir, %w", err)
	}

	dataDB, err := makeDB(ctx, opts)
	if err != nil {
		return fmt.Errorf("can't make db, %w", err)
	}

	// make detector with all sample files loaded
	detector := makeDetector(opts)

	// make spam bot
	spamBot, err := makeSpamBot(ctx, opts, dataDB, detector)
	if err != nil {
		return fmt.Errorf("can't make spam bot, %w", err)
	}
	if opts.Convert == "only" {
		log.Print("[WARN] convert only mode, converting text samples and exit")
		return nil
	}

	// make store and load approved users
	approvedUsersStore, auErr := storage.NewApprovedUsers(ctx, dataDB)
	if auErr != nil {
		return fmt.Errorf("can't make approved users store, %w", auErr)
	}

	count, err := detector.WithUserStorage(approvedUsersStore)
	if err != nil {
		return fmt.Errorf("can't load approved users, %w", err)
	}
	log.Printf("[DEBUG] approved users loaded: %d", count)

	// make locator
	locator, err := storage.NewLocator(ctx, opts.HistoryDuration, opts.HistoryMinSize, dataDB)
	if err != nil {
		return fmt.Errorf("can't make locator, %w", err)
	}

	// activate web server if enabled
	if opts.Server.Enabled {
		// server starts in background goroutine
		if srvErr := activateServer(ctx, opts, spamBot, locator, dataDB); srvErr != nil {
			return fmt.Errorf("can't activate web server, %w", srvErr)
		}
		// if no telegram token and group set, just run the server
		if opts.Telegram.Token == "" || opts.Telegram.Group == "" {
			log.Printf("[WARN] no telegram token and group set, web server only mode")
			<-ctx.Done()
			return nil
		}
	}

	// make telegram bot
	tbAPI, err := tbapi.NewBotAPI(opts.Telegram.Token)
	if err != nil {
		return fmt.Errorf("can't make telegram bot, %w", err)
	}
	tbAPI.Debug = opts.TGDbg

	// make spam logger writer
	loggerWr, err := makeSpamLogWriter(opts)
	if err != nil {
		return fmt.Errorf("can't make spam log writer, %w", err)
	}
	defer loggerWr.Close()

	// make spam logger
	spamLogger, err := makeSpamLogger(ctx, opts.InstanceID, loggerWr, dataDB)
	if err != nil {
		return fmt.Errorf("can't make spam logger, %w", err)
	}

	// make telegram listener
	tgListener := events.TelegramListener{
		TbAPI:                   tbAPI,
		Group:                   opts.Telegram.Group,
		IdleDuration:            opts.Telegram.IdleDuration,
		SuperUsers:              opts.SuperUsers,
		Bot:                     spamBot,
		StartupMsg:              opts.Message.Startup,
		WarnMsg:                 opts.Message.Warn,
		NoSpamReply:             opts.NoSpamReply,
		SuppressJoinMessage:     opts.SuppressJoinMessage,
		SpamLogger:              spamLogger,
		AdminGroup:              opts.AdminGroup,
		TestingIDs:              opts.TestingIDs,
		Locator:                 locator,
		TrainingMode:            opts.Training,
		SoftBanMode:             opts.SoftBan,
		DisableAdminSpamForward: opts.DisableAdminSpamForward,
		Dry:                     opts.Dry,
	}

	log.Printf("[DEBUG] telegram listener config: {group: %s, idle: %v, super: %v, admin: %s, testing: %v, no-reply: %v,"+
		" suppress: %v, dry: %v, training: %v}", tgListener.Group, tgListener.IdleDuration, tgListener.SuperUsers,
		tgListener.AdminGroup, tgListener.TestingIDs, tgListener.NoSpamReply, tgListener.SuppressJoinMessage, tgListener.Dry,
		tgListener.TrainingMode)

	// run telegram listener and event processor loop
	if err := tgListener.Do(ctx); err != nil {
		return fmt.Errorf("telegram listener failed, %w", err)
	}
	return nil
}

// makeDB creates database connection based on options
// if dbURL is a file name, uses sqlite with dynamic data path, otherwise uses dbURL as is
func makeDB(ctx context.Context, opts options) (*engine.SQL, error) {
	if opts.DataBaseURL == "" {
		return nil, errors.New("empty database URL")
	}
	dbURL := opts.DataBaseURL // default to what is set in options

	// if dbURL has no path separator, assume it is a file name and add dynamic data path for sqlite
	if !strings.Contains(dbURL, "/") && !strings.Contains(dbURL, "\\") {
		dbURL = filepath.Join(opts.Files.DynamicDataPath, dbURL)
	}
	log.Printf("[DEBUG] data db: %s", dbURL)

	db, err := engine.New(ctx, dbURL, opts.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("can't make db %s, %w", opts.DataBaseURL, err)
	}

	// backup db on version change for sqlite
	if db.Type() == engine.Sqlite {
		// get file name from dbURL for sqlite
		dbFile := dbURL
		dbFile = strings.TrimPrefix(dbFile, "file://")
		dbFile = strings.TrimPrefix(dbFile, "file:")

		// make backup of db on version change for sqlite
		if opts.MaxBackups > 0 {
			if err := backupDB(dbFile, revision, opts.MaxBackups); err != nil {
				return nil, fmt.Errorf("backup on version change failed, %w", err)
			}
		} else {
			log.Print("[WARN] database backups disabled")
		}
	}
	return db, nil
}

// checkVolumeMount checks if dynamic files location mounted in docker and shows warning if not
// returns true if running not in docker or dynamic files dir mounted
func checkVolumeMount(opts options) (ok bool) {
	if os.Getenv("TGSPAM_IN_DOCKER") != "1" {
		return true
	}
	log.Printf("[DEBUG] running in docker")
	warnMsg := fmt.Sprintf("dynamic files dir %q is not mounted, changes will be lost on container restart", opts.Files.DynamicDataPath)

	// check if dynamic files dir not present. This means it is not mounted
	_, err := os.Stat(opts.Files.DynamicDataPath)
	if err != nil {
		log.Printf("[WARN] %s", warnMsg)
		// no dynamic files dir, no need to check further
		return false
	}

	// check if .not_mounted file missing, this means it is mounted
	if _, err = os.Stat(filepath.Join(opts.Files.DynamicDataPath, ".not_mounted")); err != nil {
		return true
	}

	// if .not_mounted file present, it can be mounted anyway with docker named volumes
	output, err := exec.Command("mount").Output()
	if err != nil {
		log.Printf("[WARN] %s, can't check mount: %v", warnMsg, err)
		return true
	}
	// check if the output contains the specified directory
	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(line, opts.Files.DynamicDataPath) {
			return true
		}
	}

	log.Printf("[WARN] %s", warnMsg)
	return false
}

func activateServer(ctx context.Context, opts options, sf *bot.SpamFilter, loc *storage.Locator, db *engine.SQL) (err error) {
	authPassswd := opts.Server.AuthPasswd
	if opts.Server.AuthPasswd == "auto" {
		authPassswd, err = webapi.GenerateRandomPassword(20)
		if err != nil {
			return fmt.Errorf("can't generate random password, %w", err)
		}
		authHash, err := rest.GenerateBcryptHash(authPassswd)
		if err != nil {
			return fmt.Errorf("can't generate bcrypt hash for password, %w", err)
		}
		log.Printf("[WARN] generated basic auth password for user tg-spam: %q, bcrypt hash: %s", authPassswd, authHash)
	}

	// make store and load approved users
	detectedSpamStore, dsErr := storage.NewDetectedSpam(ctx, db)
	if dsErr != nil {
		return fmt.Errorf("can't make detected spam store, %w", dsErr)
	}

	settings := webapi.Settings{
		InstanceID:              opts.InstanceID,
		PrimaryGroup:            opts.Telegram.Group,
		AdminGroup:              opts.AdminGroup,
		DisableAdminSpamForward: opts.DisableAdminSpamForward,
		LoggerEnabled:           opts.Logger.Enabled,
		SuperUsers:              opts.SuperUsers,
		StorageTimeout:          opts.StorageTimeout,
		NoSpamReply:             opts.NoSpamReply,
		CasEnabled:              opts.CAS.API != "",
		MetaEnabled:             opts.Meta.ImageOnly || opts.Meta.LinksLimit >= 0 || opts.Meta.MentionsLimit >= 0 || opts.Meta.LinksOnly || opts.Meta.VideosOnly || opts.Meta.AudiosOnly || opts.Meta.Forward || opts.Meta.Keyboard || opts.Meta.UsernameSymbols != "",
		MetaLinksLimit:          opts.Meta.LinksLimit,
		MetaMentionsLimit:       opts.Meta.MentionsLimit,
		MetaLinksOnly:           opts.Meta.LinksOnly,
		MetaImageOnly:           opts.Meta.ImageOnly,
		MetaVideoOnly:           opts.Meta.VideosOnly,
		MetaAudioOnly:           opts.Meta.AudiosOnly,
		MetaForwarded:           opts.Meta.Forward,
		MetaKeyboard:            opts.Meta.Keyboard,
		MetaUsernameSymbols:     opts.Meta.UsernameSymbols,
		MultiLangLimit:          opts.MultiLangWords,
		OpenAIEnabled:           opts.OpenAI.Token != "" || opts.OpenAI.APIBase != "",
		OpenAIVeto:              opts.OpenAI.Veto,
		OpenAIHistorySize:       opts.OpenAI.HistorySize,
		OpenAIModel:             opts.OpenAI.Model,
		OpenAICustomPrompts:     opts.OpenAI.CustomPrompts,
		LuaPluginsEnabled:       opts.LuaPlugins.Enabled,
		LuaPluginsDir:           opts.LuaPlugins.PluginsDir,
		LuaEnabledPlugins:       opts.LuaPlugins.EnabledPlugins,
		LuaDynamicReload:        opts.LuaPlugins.DynamicReload,
		SamplesDataPath:         opts.Files.SamplesDataPath,
		DynamicDataPath:         opts.Files.DynamicDataPath,
		WatchIntervalSecs:       int(opts.Files.WatchInterval.Seconds()),
		SimilarityThreshold:     opts.SimilarityThreshold,
		MinMsgLen:               opts.MinMsgLen,
		MaxEmoji:                opts.MaxEmoji,
		MinSpamProbability:      opts.MinSpamProbability,
		ParanoidMode:            opts.ParanoidMode,
		FirstMessagesCount:      opts.FirstMessagesCount,
		StartupMessageEnabled:   opts.Message.Startup != "",
		TrainingEnabled:         opts.Training,
		SoftBanEnabled:          opts.SoftBan,
		AbnormalSpacingEnabled:  opts.AbnormalSpacing.Enabled,
		HistorySize:             opts.HistorySize,
		DebugModeEnabled:        opts.Dbg,
		DryModeEnabled:          opts.Dry,
		TGDebugModeEnabled:      opts.TGDbg,
	}

	srv := webapi.Server{Config: webapi.Config{
		ListenAddr:    opts.Server.ListenAddr,
		Detector:      sf.Detector,
		SpamFilter:    sf,
		Locator:       loc,
		DetectedSpam:  detectedSpamStore,
		StorageEngine: db, // add database engine for backup functionality
		AuthPasswd:    authPassswd,
		AuthHash:      opts.Server.AuthHash,
		Version:       revision,
		Dbg:           opts.Dbg,
		Settings:      settings,
	}}

	go func() {
		if err := srv.Run(ctx); err != nil {
			log.Printf("[ERROR] web server failed, %v", err)
		}
	}()
	return nil
}

// makeDetector creates spam detector with all checkers and updaters
// it loads samples and dynamic files
func makeDetector(opts options) *tgspam.Detector {
	detectorConfig := tgspam.Config{
		MaxAllowedEmoji:     opts.MaxEmoji,
		MinMsgLen:           opts.MinMsgLen,
		SimilarityThreshold: opts.SimilarityThreshold,
		MinSpamProbability:  opts.MinSpamProbability,
		CasAPI:              opts.CAS.API,
		CasUserAgent:        opts.CAS.UserAgent,
		HTTPClient:          &http.Client{Timeout: opts.CAS.Timeout},
		FirstMessageOnly:    !opts.ParanoidMode,
		FirstMessagesCount:  opts.FirstMessagesCount,
		OpenAIVeto:          opts.OpenAI.Veto,
		OpenAIHistorySize:   opts.OpenAI.HistorySize, // how many last requests sent to openai
		MultiLangWords:      opts.MultiLangWords,
		HistorySize:         opts.HistorySize, // how many last request stored in memory
	}

	// FirstMessagesCount and ParanoidMode are mutually exclusive.
	// ParanoidMode still here for backward compatibility only.
	if opts.FirstMessagesCount > 0 { // if FirstMessagesCount is set, FirstMessageOnly is enforced
		detectorConfig.FirstMessageOnly = true
	}
	if opts.ParanoidMode { // if ParanoidMode is set, FirstMessagesCount is ignored
		detectorConfig.FirstMessageOnly = false
		detectorConfig.FirstMessagesCount = 0
	}
	if opts.StorageTimeout > 0 { // if StorageTimeout is non-zero, set it. If zero, storage timeout is disabled
		detectorConfig.StorageTimeout = opts.StorageTimeout
	}

	detector := tgspam.NewDetector(detectorConfig)

	if opts.OpenAI.Token != "" || opts.OpenAI.APIBase != "" {
		log.Printf("[WARN] openai enabled")
		openAIConfig := tgspam.OpenAIConfig{
			SystemPrompt:      opts.OpenAI.Prompt,
			CustomPrompts:     opts.OpenAI.CustomPrompts,
			Model:             opts.OpenAI.Model,
			MaxTokensResponse: opts.OpenAI.MaxTokensResponse,
			MaxTokensRequest:  opts.OpenAI.MaxTokensRequest,
			MaxSymbolsRequest: opts.OpenAI.MaxSymbolsRequest,
			RetryCount:        opts.OpenAI.RetryCount,
			ReasoningEffort:   opts.OpenAI.ReasoningEffort,
		}

		config := openai.DefaultConfig(opts.OpenAI.Token)
		if opts.OpenAI.APIBase != "" {
			config.BaseURL = opts.OpenAI.APIBase
		}
		log.Printf("[DEBUG] openai config: %+v", openAIConfig)

		detector.WithOpenAIChecker(openai.NewClientWithConfig(config), openAIConfig)
	}

	if opts.AbnormalSpacing.Enabled {
		log.Printf("[INFO] words spacing check enabled")
		detector.AbnormalSpacing.Enabled = true
		detector.AbnormalSpacing.ShortWordLen = opts.AbnormalSpacing.ShortWordLen
		detector.AbnormalSpacing.ShortWordRatioThreshold = opts.AbnormalSpacing.ShortWordRatioThreshold
		detector.AbnormalSpacing.SpaceRatioThreshold = opts.AbnormalSpacing.SpaceRatioThreshold
		detector.AbnormalSpacing.MinWordsCount = opts.AbnormalSpacing.MinWords
	}

	metaChecks := []tgspam.MetaCheck{}
	if opts.Meta.ImageOnly {
		log.Printf("[INFO] image only check enabled")
		metaChecks = append(metaChecks, tgspam.ImagesCheck())
	}
	if opts.Meta.VideosOnly {
		log.Printf("[INFO] videos only check enabled")
		metaChecks = append(metaChecks, tgspam.VideosCheck())
	}
	if opts.Meta.LinksLimit >= 0 {
		log.Printf("[INFO] links check enabled, limit: %d", opts.Meta.LinksLimit)
		metaChecks = append(metaChecks, tgspam.LinksCheck(opts.Meta.LinksLimit))
	}
	if opts.Meta.MentionsLimit >= 0 {
		log.Printf("[INFO] mentions check enabled, limit: %d", opts.Meta.MentionsLimit)
		metaChecks = append(metaChecks, tgspam.MentionsCheck(opts.Meta.MentionsLimit))
	}
	if opts.Meta.LinksOnly {
		log.Printf("[INFO] links only check enabled")
		metaChecks = append(metaChecks, tgspam.LinkOnlyCheck())
	}
	if opts.Meta.Forward {
		log.Printf("[INFO] forward check enabled")
		metaChecks = append(metaChecks, tgspam.ForwardedCheck())
	}
	if opts.Meta.Keyboard {
		log.Printf("[INFO] keyboard check enabled")
		metaChecks = append(metaChecks, tgspam.KeyboardCheck())
	}
	if opts.Meta.UsernameSymbols != "" {
		log.Printf("[INFO] username symbols check enabled, prohibited symbols: %q", opts.Meta.UsernameSymbols)
		metaChecks = append(metaChecks, tgspam.UsernameSymbolsCheck(opts.Meta.UsernameSymbols))
	}
	detector.WithMetaChecks(metaChecks...)

	log.Printf("[DEBUG] detector config: %+v", detectorConfig)

	// initialize Lua plugins if enabled
	if opts.LuaPlugins.Enabled {
		// copy Lua plugin settings to detector config
		detector.LuaPlugins.Enabled = true
		detector.LuaPlugins.PluginsDir = opts.LuaPlugins.PluginsDir
		detector.LuaPlugins.EnabledPlugins = opts.LuaPlugins.EnabledPlugins
		detector.LuaPlugins.DynamicReload = opts.LuaPlugins.DynamicReload

		// create and initialize the plugin engine
		luaEngine := plugin.NewChecker()
		if err := detector.WithLuaEngine(luaEngine); err != nil {
			log.Printf("[WARN] failed to initialize Lua plugins: %v", err)
		} else {
			log.Printf("[INFO] lua plugins enabled from directory: %s", opts.LuaPlugins.PluginsDir)
			if len(opts.LuaPlugins.EnabledPlugins) > 0 {
				log.Printf("[INFO] enabled Lua plugins: %v", opts.LuaPlugins.EnabledPlugins)
			} else {
				log.Print("[INFO] all Lua plugins from directory are enabled")
			}

			if opts.LuaPlugins.DynamicReload {
				log.Print("[INFO] dynamic reloading of Lua plugins enabled")
			}
		}
	}

	return detector
}

func makeSpamBot(ctx context.Context, opts options, dataDB *engine.SQL, detector *tgspam.Detector) (*bot.SpamFilter, error) {
	if dataDB == nil || detector == nil {
		return nil, errors.New("nil datadb or detector")
	}

	// make samples store
	samplesStore, err := storage.NewSamples(ctx, dataDB)
	if err != nil {
		return nil, fmt.Errorf("can't make samples store, %w", err)
	}
	if err = migrateSamples(ctx, opts, samplesStore); err != nil {
		return nil, fmt.Errorf("can't migrate samples, %w", err)
	}

	// make dictionary store
	dictionaryStore, err := storage.NewDictionary(ctx, dataDB)
	if err != nil {
		return nil, fmt.Errorf("can't make dictionary store, %w", err)
	}
	if err := migrateDicts(ctx, opts, dictionaryStore); err != nil {
		return nil, fmt.Errorf("can't migrate dictionary, %w", err)
	}

	spamBotParams := bot.SpamConfig{
		GroupID:      opts.InstanceID,
		SamplesStore: samplesStore,
		DictStore:    dictionaryStore,
		SpamMsg:      opts.Message.Spam,
		SpamDryMsg:   opts.Message.Dry,
		Dry:          opts.Dry,
	}
	spamBot := bot.NewSpamFilter(detector, spamBotParams)
	log.Printf("[DEBUG] spam bot config: %+v", spamBotParams)

	if err := spamBot.ReloadSamples(); err != nil {
		return nil, fmt.Errorf("can't reload samples, %w", err)
	}

	// set detector samples updaters
	detector.WithSpamUpdater(storage.NewSampleUpdater(samplesStore, storage.SampleTypeSpam, opts.StorageTimeout))
	detector.WithHamUpdater(storage.NewSampleUpdater(samplesStore, storage.SampleTypeHam, opts.StorageTimeout))

	return spamBot, nil
}

// expandPath expands ~ to home dir and makes the absolute path
func expandPath(path string) string {
	if path == "" {
		return ""
	}
	if path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return filepath.Join(home, path[1:])
	}
	ep, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return ep
}

type nopWriteCloser struct{ io.Writer }

func (n nopWriteCloser) Close() error { return nil }

// makeSpamLogger creates spam logger to keep reports about spam messages
// it writes json lines to the provided writer
func makeSpamLogger(ctx context.Context, gid string, wr io.Writer, dataDB *engine.SQL) (events.SpamLogger, error) {
	// make store and load approved users
	detectedSpamStore, auErr := storage.NewDetectedSpam(ctx, dataDB)
	if auErr != nil {
		return nil, fmt.Errorf("can't make approved users store, %w", auErr)
	}

	logWr := events.SpamLoggerFunc(func(msg *bot.Message, response *bot.Response) {
		userName := msg.From.Username
		if userName == "" {
			userName = msg.From.DisplayName
		}
		// write to log file
		text := strings.ReplaceAll(msg.Text, "\n", " ")
		text = strings.TrimSpace(text)
		log.Printf("[DEBUG] spam detected from %v, text: %s", msg.From, text)
		m := struct {
			TimeStamp   string `json:"ts"`
			DisplayName string `json:"display_name"`
			UserName    string `json:"user_name"`
			UserID      int64  `json:"user_id"`
			Text        string `json:"text"`
		}{
			TimeStamp:   time.Now().In(time.Local).Format(time.RFC3339),
			DisplayName: msg.From.DisplayName,
			UserName:    msg.From.Username,
			UserID:      msg.From.ID,
			Text:        text,
		}
		line, err := json.Marshal(&m)
		if err != nil {
			log.Printf("[WARN] can't marshal json, %v", err)
			return
		}
		if _, err := wr.Write(append(line, '\n')); err != nil {
			log.Printf("[WARN] can't write to log, %v", err)
		}

		// write to db store
		rec := storage.DetectedSpamInfo{
			Text:      text,
			UserID:    msg.From.ID,
			UserName:  userName,
			Timestamp: time.Now().In(time.Local),
			GID:       gid,
		}
		if err := detectedSpamStore.Write(ctx, rec, response.CheckResults); err != nil {
			log.Printf("[WARN] can't write to db, %v", err)
		}
	})

	return logWr, nil
}

// makeSpamLogWriter creates spam log writer to keep reports about spam messages
// it parses options and makes lumberjack logger with rotation
func makeSpamLogWriter(opts options) (accessLog io.WriteCloser, err error) {
	if !opts.Logger.Enabled {
		return nopWriteCloser{io.Discard}, nil
	}

	sizeParse := func(inp string) (uint64, error) {
		if inp == "" {
			return 0, errors.New("empty value")
		}
		for i, sfx := range []string{"k", "m", "g", "t"} {
			if strings.HasSuffix(inp, strings.ToUpper(sfx)) || strings.HasSuffix(inp, strings.ToLower(sfx)) {
				val, err := strconv.Atoi(inp[:len(inp)-1])
				if err != nil {
					return 0, fmt.Errorf("can't parse %s: %w", inp, err)
				}
				return uint64(float64(val) * math.Pow(float64(1024), float64(i+1))), nil
			}
		}
		return strconv.ParseUint(inp, 10, 64)
	}

	maxSize, perr := sizeParse(opts.Logger.MaxSize)
	if perr != nil {
		return nil, fmt.Errorf("can't parse logger MaxSize: %w", perr)
	}

	maxSize /= 1048576

	log.Printf("[INFO] logger enabled for %s, max size %dM", opts.Logger.FileName, maxSize)
	return &lumberjack.Logger{
		Filename:   opts.Logger.FileName,
		MaxSize:    int(maxSize), //nolint:gosec // size in MB not that big to cause overflow
		MaxBackups: opts.Logger.MaxBackups,
		Compress:   true,
		LocalTime:  true,
	}, nil
}

// migrateSamples runs migrations from legacy text files samples to db, if such files found
func migrateSamples(ctx context.Context, opts options, samplesDB *storage.Samples) error {
	if opts.Convert == "disabled" {
		log.Print("[DEBUG] samples migration disabled")
		return nil
	}
	migrateSamples := func(file string, sampleType storage.SampleType, origin storage.SampleOrigin) (*storage.SamplesStats, error) {
		if _, err := os.Stat(file); err != nil {
			log.Printf("[DEBUG] samples file %s not found, skip", file)
			return &storage.SamplesStats{}, nil
		}
		fh, err := os.Open(file) //nolint:gosec // file path is controlled by the app
		if err != nil {
			return nil, fmt.Errorf("can't open samples file, %w", err)
		}
		defer fh.Close()
		stats, err := samplesDB.Import(ctx, sampleType, origin, fh, true) // clean records before import
		if err != nil {
			return nil, fmt.Errorf("can't load samples, %w", err)
		}
		if err := fh.Close(); err != nil {
			return nil, fmt.Errorf("can't close samples file, %w", err)
		}
		if err := os.Rename(file, file+".loaded"); err != nil {
			return nil, fmt.Errorf("can't rename samples file, %w", err)
		}
		return stats, nil
	}

	if samplesDB == nil {
		return errors.New("samples db is nil")
	}

	// migrate preset spam samples if files exist
	spamPresetFile := filepath.Join(opts.Files.SamplesDataPath, samplesSpamFile)
	s, err := migrateSamples(spamPresetFile, storage.SampleTypeSpam, storage.SampleOriginPreset)
	if err != nil {
		return fmt.Errorf("can't migrate spam preset samples, %w", err)
	}
	if s.PresetHam > 0 {
		log.Printf("[DEBUG] spam preset samples loaded: %s", s)
	}

	// migrate preset ham samples if files exist
	hamPresetFile := filepath.Join(opts.Files.SamplesDataPath, samplesHamFile)
	s, err = migrateSamples(hamPresetFile, storage.SampleTypeHam, storage.SampleOriginPreset)
	if err != nil {
		return fmt.Errorf("can't migrate ham preset samples, %w", err)
	}
	if s.PresetHam > 0 {
		log.Printf("[DEBUG] ham preset samples loaded: %s", s)
	}

	// migrate dynamic spam samples if files exist
	dynSpamFile := filepath.Join(opts.Files.DynamicDataPath, dynamicSpamFile)
	s, err = migrateSamples(dynSpamFile, storage.SampleTypeSpam, storage.SampleOriginUser)
	if err != nil {
		return fmt.Errorf("can't migrate spam dynamic samples, %w", err)
	}
	if s.UserSpam > 0 {
		log.Printf("[DEBUG] spam dynamic samples loaded: %s", s)
	}

	// migrate dynamic ham samples if files exist
	dynHamFile := filepath.Join(opts.Files.DynamicDataPath, dynamicHamFile)
	s, err = migrateSamples(dynHamFile, storage.SampleTypeHam, storage.SampleOriginUser)
	if err != nil {
		return fmt.Errorf("can't migrate ham dynamic samples, %w", err)
	}
	if s.UserHam > 0 {
		log.Printf("[DEBUG] ham dynamic samples loaded: %s", s)
	}

	if s.TotalHam > 0 || s.TotalSpam > 0 {
		log.Printf("[INFO] samples migration done: %s", s)
	}
	return nil
}

// migrateDicts runs migrations from legacy dictionary text files to db, if needed
func migrateDicts(ctx context.Context, opts options, dictDB *storage.Dictionary) error {
	if opts.Convert == "disabled" {
		log.Print("[DEBUG] dictionary migration disabled")
		return nil
	}

	migrateDict := func(file string, dictType storage.DictionaryType) (*storage.DictionaryStats, error) {
		if _, err := os.Stat(file); err != nil {
			log.Printf("[DEBUG] dictionary file %s not found, skip", file)
			return &storage.DictionaryStats{}, nil
		}
		fh, err := os.Open(file) //nolint:gosec // file path is controlled by the app
		if err != nil {
			return nil, fmt.Errorf("can't open dictionary file, %w", err)
		}
		defer fh.Close()
		stats, err := dictDB.Import(ctx, dictType, fh, true) // clean records before import
		if err != nil {
			return nil, fmt.Errorf("can't load dictionary, %w", err)
		}
		if err := fh.Close(); err != nil {
			return nil, fmt.Errorf("can't close dictionary file, %w", err)
		}
		if err := os.Rename(file, file+".loaded"); err != nil {
			return nil, fmt.Errorf("can't rename dictionary file, %w", err)
		}
		return stats, nil
	}

	if dictDB == nil {
		return errors.New("dictionary db is nil")
	}

	// migrate stop-words if files exist
	stopWordsFile := filepath.Join(opts.Files.SamplesDataPath, stopWordsFile)
	s, err := migrateDict(stopWordsFile, storage.DictionaryTypeStopPhrase)
	if err != nil {
		return fmt.Errorf("can't migrate stop words, %w", err)
	}
	if s.TotalStopPhrases > 0 {
		log.Printf("[INFO] stop words loaded: %s", s)
	}

	// migrate excluded tokens if files exist
	excludeTokensFile := filepath.Join(opts.Files.SamplesDataPath, excludeTokensFile)
	s, err = migrateDict(excludeTokensFile, storage.DictionaryTypeIgnoredWord)
	if err != nil {
		return fmt.Errorf("can't migrate excluded tokens, %w", err)
	}
	if s.TotalIgnoredWords > 0 {
		log.Printf("[INFO] excluded tokens loaded: %s", s)
	}

	if s.TotalIgnoredWords > 0 || s.TotalStopPhrases > 0 {
		log.Printf("[DEBUG] dictionaries migration done: %s", s)
	}
	return nil
}

// backupDB creates a backup of the db file if the version has changed. It copies the db file to a new db file
// named as the original file with a version suffix, e.g., tg-spam.db.master-77e0bfd-20250107T23:17:34.
// The file is created only if the version has changed and a backup file with the name tg-spam.db.<version> does not exist.
// It keeps up to maxBackups files; if maxBackups is 0, no backups are made.
// Files are removed based on the final part of the version, i.e., 20250107T23:17:34, with the oldest backups removed first.
// If the backup file extension suffix with the timestamp is not found, the modification time of the file is used instead.
func backupDB(dbFile, version string, maxBackups int) error {
	if maxBackups == 0 {
		return nil
	}
	backupFile := dbFile + "." + strings.ReplaceAll(version, ".", "_") // replace dots with underscores for file name
	if _, err := os.Stat(backupFile); err == nil {
		// backup file for the version already exists, no need to make it again
		return nil
	}
	if _, err := os.Stat(dbFile); err != nil {
		// db file not found, no need to backup. This is legit if the db is not created yet on the first run
		log.Printf("[WARN] db file not found: %s, skip backup", dbFile)
		return nil
	}

	log.Printf("[DEBUG] db backup: %s -> %s", dbFile, backupFile)
	// copy current db to the backup file
	if err := fileutils.CopyFile(dbFile, backupFile); err != nil {
		return fmt.Errorf("failed to copy db file: %w", err)
	}
	log.Printf("[INFO] db backup created: %s", backupFile)

	// cleanup old backups if needed
	files, err := filepath.Glob(dbFile + ".*")
	if err != nil {
		return fmt.Errorf("failed to list backup files: %w", err)
	}

	if len(files) <= maxBackups {
		return nil
	}

	// sort files by timestamp in version suffix or mod time if suffix not formatted as timestamp
	sort.Slice(files, func(i, j int) bool {
		getTime := func(f string) time.Time {
			base := filepath.Base(f) // file name like this: tg-spam.db.master-77e0bfd-20250107T23:17:34
			// try to get timestamp from version suffix first
			parts := strings.Split(base, "-")
			if len(parts) >= 3 {
				suffix := parts[len(parts)-1]
				if t, err := time.ParseInLocation("20060102T15:04:05", suffix, time.Local); err == nil {
					return t
				}
			}
			// fallback to modification time for non-versioned files
			fi, err := os.Stat(f)
			if err != nil {
				log.Printf("[WARN] can't stat file %s: %v", f, err)
				return time.Now().Local() // treat errored files as newest to avoid deleting them
			}
			return fi.ModTime().Local() // convert to local for consistent comparison
		}
		return getTime(files[i]).Before(getTime(files[j]))
	})

	// remove oldest files
	for i := 0; i < len(files)-maxBackups; i++ {
		if err := os.Remove(files[i]); err != nil {
			return fmt.Errorf("failed to remove old backup %s: %w", files[i], err)
		}
		log.Printf("[DEBUG] db backup removed: %s", files[i])
	}
	return nil
}

func setupLog(dbg bool, secrets ...string) {
	logOpts := []lgr.Option{lgr.Msec, lgr.LevelBraces, lgr.StackTraceOnError}
	if dbg {
		logOpts = []lgr.Option{lgr.Debug, lgr.CallerFile, lgr.CallerFunc, lgr.Msec, lgr.LevelBraces, lgr.StackTraceOnError}
	}

	colorizer := lgr.Mapper{
		ErrorFunc:  func(s string) string { return color.New(color.FgHiRed).Sprint(s) },
		WarnFunc:   func(s string) string { return color.New(color.FgRed).Sprint(s) },
		InfoFunc:   func(s string) string { return color.New(color.FgYellow).Sprint(s) },
		DebugFunc:  func(s string) string { return color.New(color.FgWhite).Sprint(s) },
		CallerFunc: func(s string) string { return color.New(color.FgBlue).Sprint(s) },
		TimeFunc:   func(s string) string { return color.New(color.FgCyan).Sprint(s) },
	}
	logOpts = append(logOpts, lgr.Map(colorizer))

	if len(secrets) > 0 {
		logOpts = append(logOpts, lgr.Secret(secrets...))
	}
	lgr.SetupStdLogger(logOpts...)
	lgr.Setup(logOpts...)
}
