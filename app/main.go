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
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/go-pkgz/lgr"
	tbapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/sashabaranov/go-openai"
	"github.com/umputun/go-flags"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/umputun/tg-spam/app/bot"
	"github.com/umputun/tg-spam/app/events"
	"github.com/umputun/tg-spam/app/storage"
	"github.com/umputun/tg-spam/lib"
)

type options struct {
	Telegram struct {
		Token        string        `long:"token" env:"TOKEN" description:"telegram bot token" required:"true"`
		Group        string        `long:"group" env:"GROUP" description:"group name/id" required:"true"`
		Timeout      time.Duration `long:"timeout" env:"TIMEOUT" default:"30s" description:"http client timeout for telegram" `
		IdleDuration time.Duration `long:"idle" env:"IDLE" default:"30s" description:"idle duration"`
	} `group:"telegram" namespace:"telegram" env-namespace:"TELEGRAM"`

	AdminGroup      string        `long:"admin.group" env:"ADMIN_GROUP" description:"admin group name, or channel id"`
	TestingIDs      []int64       `long:"testing-id" env:"TESTING_ID" env-delim:"," description:"testing ids, allow bot to reply to them"`
	HistoryDuration time.Duration `long:"history-duration" env:"HISTORY_DURATION" default:"1h" description:"history duration"`
	HistoryMinSize  int           `long:"history-min-size" env:"HISTORY_MIN_SIZE" default:"1000" description:"history minimal size to keep"`

	Logger struct {
		Enabled    bool   `long:"enabled" env:"ENABLED" description:"enable spam rotated logs"`
		FileName   string `long:"file" env:"FILE"  default:"tg-spam.log" description:"location of spam log"`
		MaxSize    string `long:"max-size" env:"MAX_SIZE" default:"100M" description:"maximum size before it gets rotated"`
		MaxBackups int    `long:"max-backups" env:"MAX_BACKUPS" default:"10" description:"maximum number of old log files to retain"`
	} `group:"logger" namespace:"logger" env-namespace:"LOGGER"`

	SuperUsers  events.SuperUser `long:"super" env:"SUPER_USER" env-delim:"," description:"super-users"`
	NoSpamReply bool             `long:"no-spam-reply" env:"NO_SPAM_REPLY" description:"do not reply to spam messages"`

	CAS struct {
		API     string        `long:"api" env:"API" default:"https://api.cas.chat" description:"CAS API"`
		Timeout time.Duration `long:"timeout" env:"TIMEOUT" default:"5s" description:"CAS timeout"`
	} `group:"cas" namespace:"cas" env-namespace:"CAS"`

	OpenAI struct {
		Token                            string `long:"token" env:"TOKEN" description:"openai token, disabled if not set"`
		Prompt                           string `long:"prompt" env:"PROMPT" default:"" description:"openai system prompt, if empty uses builtin default"`
		Model                            string `long:"model" env:"MODEL" default:"gpt-4" description:"openai model"`
		MaxTokensResponse                int    `long:"max-tokens-response" env:"MAX_TOKENS_RESPONSE" default:"1024" description:"openai max tokens in response"`
		MaxTokensRequestMaxTokensRequest int    `long:"max-tokens-request" env:"MAX_TOKENS_REQUEST" default:"2048" description:"openai max tokens in request"`
		MaxSymbolsRequest                int    `long:"max-symbols-request" env:"MAX_SYMBOLS_REQUEST" default:"16000" description:"openai max symbols in request, failback if tokenizer failed"`
	} `group:"openai" namespace:"openai" env-namespace:"OPENAI"`

	Files struct {
		SamplesSpamFile  string        `long:"samples-spam" env:"SAMPLES_SPAM" default:"data/spam-samples.txt" description:"spam samples"`
		SamplesHamFile   string        `long:"samples-ham" env:"SAMPLES_HAM" default:"data/ham-samples.txt" description:"ham samples"`
		ExcludeTokenFile string        `long:"exclude-tokens" env:"EXCLUDE_TOKENS" default:"data/exclude-tokens.txt" description:"exclude tokens file"`
		StopWordsFile    string        `long:"stop-words" env:"STOP_WORDS" default:"data/stop-words.txt" description:"stop words file"`
		DynamicSpamFile  string        `long:"dynamic-spam" env:"DYNAMIC_SPAM" default:"data/spam-dynamic.txt" description:"dynamic spam file"`
		DynamicHamFile   string        `long:"dynamic-ham" env:"DYNAMIC_HAM" default:"data/ham-dynamic.txt" description:"dynamic ham file"`
		WatchInterval    time.Duration `long:"watch-interval" env:"WATCH_INTERVAL" default:"5s" description:"watch interval"`
		ApprovedUsers    string        `long:"approved-users" env:"APPROVED_USERS" default:"data/approved-users.txt" description:"approved users file"`
	} `group:"files" namespace:"files" env-namespace:"FILES"`

	SimilarityThreshold float64 `long:"similarity-threshold" env:"SIMILARITY_THRESHOLD" default:"0.5" description:"spam threshold"`
	MinMsgLen           int     `long:"min-msg-len" env:"MIN_MSG_LEN" default:"50" description:"min message length to check"`
	MaxEmoji            int     `long:"max-emoji" env:"MAX_EMOJI" default:"2" description:"max emoji count in message, -1 to disable check"`
	ParanoidMode        bool    `long:"paranoid" env:"PARANOID" description:"paranoid mode, check all messages"`
	FirstMessagesCount  int     `long:"first-messages-count" env:"FIRST_MESSAGES_COUNT" default:"1" description:"number of first messages to check"`
	MinSpamProbability  float64 `long:"min-probability" env:"MIN_PROBABILITY" default:"50" description:"min spam probability percent to ban"`

	Message struct {
		Startup string `long:"startup" env:"STARTUP" default:"" description:"startup message"`
		Spam    string `long:"spam" env:"SPAM" default:"this is spam" description:"spam message"`
		Dry     string `long:"dry" env:"DRY" default:"this is spam (dry mode)" description:"spam dry message"`
	} `group:"message" namespace:"message" env-namespace:"MESSAGE"`

	Training bool `long:"training" env:"TRAINING" description:"training mode, passive spam detection only"`
	Dry      bool `long:"dry" env:"DRY" description:"dry mode, no bans"`
	Dbg      bool `long:"dbg" env:"DEBUG" description:"debug mode"`
	TGDbg    bool `long:"tg-dbg" env:"TG_DEBUG" description:"telegram debug mode"`
}

var revision = "local"

func main() {
	fmt.Printf("tg-spam %s\n", revision)
	var opts options
	p := flags.NewParser(&opts, flags.PrintErrors|flags.PassDoubleDash|flags.HelpFlag)
	p.SubcommandsOptional = true
	if _, err := p.Parse(); err != nil {
		if err.(*flags.Error).Type != flags.ErrHelp {
			log.Printf("[ERROR] cli error: %v", err)
		}
		os.Exit(2)
	}

	setupLog(opts.Dbg, opts.Telegram.Token, opts.OpenAI.Token)
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

	if err := execute(ctx, opts); err != nil {
		log.Printf("[ERROR] %v", err)
		os.Exit(1)
	}
}

func execute(ctx context.Context, opts options) error {
	if opts.Dry {
		log.Print("[WARN] dry mode, no actual bans")
	}

	// make telegram bot
	tbAPI, err := tbapi.NewBotAPI(opts.Telegram.Token)
	if err != nil {
		return fmt.Errorf("can't make telegram bot, %w", err)
	}
	tbAPI.Debug = opts.TGDbg

	// make detector with all sample files loaded
	detector := makeDetector(opts)

	// load approved users and start auto-save
	if opts.Files.ApprovedUsers != "" {
		approvedUsersStore := storage.NewApprovedUsers(opts.Files.ApprovedUsers)
		defer func() {
			if serr := approvedUsersStore.Store(detector.ApprovedUsers()); serr != nil {
				log.Printf("[WARN] can't save approved users, %v", serr)
			}
		}()
		count, lerr := detector.LoadApprovedUsers(approvedUsersStore)
		if lerr != nil {
			log.Printf("[WARN] can't load approved users, %v", lerr)
		} else {
			log.Printf("[DEBUG] approved users file: %s, loaded: %d", opts.Files.ApprovedUsers, count)
		}
		go autoSaveApprovedUsers(ctx, detector, approvedUsersStore, time.Minute*5)
	}

	// make spam bot
	spamBot, err := makeSpamBot(ctx, opts, detector)
	if err != nil {
		return fmt.Errorf("can't make spam bot, %w", err)
	}

	// make spam logger
	loggerWr, err := makeSpamLogWriter(opts)
	if err != nil {
		return fmt.Errorf("can't make spam log writer, %w", err)
	}
	defer loggerWr.Close()

	// make telegram listener
	tgListener := events.TelegramListener{
		TbAPI:        tbAPI,
		Group:        opts.Telegram.Group,
		IdleDuration: opts.Telegram.IdleDuration,
		SuperUsers:   opts.SuperUsers,
		Bot:          spamBot,
		StartupMsg:   opts.Message.Startup,
		NoSpamReply:  opts.NoSpamReply,
		SpamLogger:   makeSpamLogger(loggerWr),
		AdminGroup:   opts.AdminGroup,
		TestingIDs:   opts.TestingIDs,
		Locator:      events.NewLocator(opts.HistoryDuration, opts.HistoryMinSize),
		TrainingMode: opts.Training,
		Dry:          opts.Dry,
	}
	log.Printf("[DEBUG] telegram listener config: {group: %s, idle: %v, super: %v, admin: %s, testing: %v, no-reply: %v, dry: %v}",
		tgListener.Group, tgListener.IdleDuration, tgListener.SuperUsers, tgListener.AdminGroup,
		tgListener.TestingIDs, tgListener.NoSpamReply, tgListener.Dry)

	// run telegram listener and event processor loop
	if err := tgListener.Do(ctx); err != nil {
		return fmt.Errorf("telegram listener failed, %w", err)
	}
	return nil
}

func makeDetector(opts options) *lib.Detector {
	detectorConfig := lib.Config{
		MaxAllowedEmoji:     opts.MaxEmoji,
		MinMsgLen:           opts.MinMsgLen,
		SimilarityThreshold: opts.SimilarityThreshold,
		MinSpamProbability:  opts.MinSpamProbability,
		CasAPI:              opts.CAS.API,
		HTTPClient:          &http.Client{Timeout: opts.CAS.Timeout},
		FirstMessageOnly:    !opts.ParanoidMode,
		FirstMessagesCount:  opts.FirstMessagesCount,
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

	detector := lib.NewDetector(detectorConfig)
	log.Printf("[DEBUG] detector config: %+v", detectorConfig)

	if opts.OpenAI.Token != "" {
		log.Printf("[WARN] openai enabled")
		openAIConfig := lib.OpenAIConfig{
			SystemPrompt:      opts.OpenAI.Prompt,
			Model:             opts.OpenAI.Model,
			MaxTokensResponse: opts.OpenAI.MaxTokensResponse,
			MaxTokensRequest:  opts.OpenAI.MaxTokensRequestMaxTokensRequest,
			MaxSymbolsRequest: opts.OpenAI.MaxSymbolsRequest,
		}
		log.Printf("[DEBUG] openai  config: %+v", openAIConfig)
		detector.WithOpenAIChecker(openai.NewClient(opts.OpenAI.Token), openAIConfig)
	}

	if opts.Files.DynamicSpamFile != "" {
		detector.WithSpamUpdater(bot.NewSampleUpdater(opts.Files.DynamicSpamFile))
		log.Printf("[DEBUG] dynamic spam file: %s", opts.Files.DynamicSpamFile)
	}
	if opts.Files.DynamicHamFile != "" {
		detector.WithHamUpdater(bot.NewSampleUpdater(opts.Files.DynamicHamFile))
		log.Printf("[DEBUG] dynamic ham file: %s", opts.Files.DynamicHamFile)
	}
	return detector
}

func makeSpamBot(ctx context.Context, opts options, detector *lib.Detector) (*bot.SpamFilter, error) {
	spamBotParams := bot.SpamConfig{
		SpamSamplesFile:    opts.Files.SamplesSpamFile,
		HamSamplesFile:     opts.Files.SamplesHamFile,
		SpamDynamicFile:    opts.Files.DynamicSpamFile,
		HamDynamicFile:     opts.Files.DynamicHamFile,
		ExcludedTokensFile: opts.Files.ExcludeTokenFile,
		StopWordsFile:      opts.Files.StopWordsFile,
		WatchDelay:         opts.Files.WatchInterval,
		SpamMsg:            opts.Message.Spam,
		SpamDryMsg:         opts.Message.Dry,
		Dry:                opts.Dry,
	}
	spamBot := bot.NewSpamFilter(ctx, detector, spamBotParams)
	log.Printf("[DEBUG] spam bot config: %+v", spamBotParams)

	if err := spamBot.ReloadSamples(); err != nil {
		return nil, fmt.Errorf("can't relaod samples, %w", err)
	}
	return spamBot, nil
}

// makeSpamLogger creates spam logger to keep reports about spam messages
// it writes json lines to the provided writer
func makeSpamLogger(wr io.Writer) events.SpamLogger {
	return events.SpamLoggerFunc(func(msg *bot.Message, response *bot.Response) {
		text := strings.ReplaceAll(msg.Text, "\n", " ")
		text = strings.TrimSpace(text)
		log.Printf("[INFO] spam detected from %v, response: %s", msg.From, text)
		log.Printf("[DEBUG] spam message: %s", text)
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
	})
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
		MaxSize:    int(maxSize), // in MB
		MaxBackups: opts.Logger.MaxBackups,
		Compress:   true,
		LocalTime:  true,
	}, nil
}

func autoSaveApprovedUsers(ctx context.Context, detector *lib.Detector, store *storage.ApprovedUsers, interval time.Duration) {
	log.Printf("[DEBUG] auto-save approved users every %v", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	lastCount := 0
	for {
		select {
		case <-ctx.Done():
			log.Printf("[DEBUG] auto-save approved users stopped")
			return
		case <-ticker.C:
			ids := detector.ApprovedUsers()
			if len(ids) == lastCount {
				continue
			}
			if err := store.Store(ids); err != nil {
				log.Printf("[WARN] can't save approved users, %v", err)
				continue
			}
			lastCount = len(ids)
		}
	}
}

type nopWriteCloser struct{ io.Writer }

func (n nopWriteCloser) Close() error { return nil }

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
