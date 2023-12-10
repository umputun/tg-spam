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
	"github.com/umputun/go-flags"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/umputun/tg-spam/app/bot"
	"github.com/umputun/tg-spam/app/events"
	"github.com/umputun/tg-spam/app/server"
	"github.com/umputun/tg-spam/lib"
)

var opts struct {
	Telegram struct {
		Token        string        `long:"token" env:"TOKEN" description:"telegram bot token" required:"true"`
		Group        string        `long:"group" env:"GROUP" description:"group name/id" required:"true"`
		Timeout      time.Duration `long:"timeout" env:"TIMEOUT" default:"30s" description:"http client timeout for telegram" `
		IdleDuration time.Duration `long:"idle" env:"IDLE" default:"30s" description:"idle duration"`
	} `group:"telegram" namespace:"telegram" env-namespace:"TELEGRAM"`

	Admin struct {
		URL     string `long:"url" env:"URL" description:"admin root url"`
		Address string `long:"address" env:"ADDRESS" default:":8080" description:"admin listen address"`
		Secret  string `long:"secret" env:"SECRET" description:"admin secret"`
		Group   string `long:"group" env:"GROUP" description:"admin group name/id"`
	} `group:"admin" namespace:"admin" env-namespace:"ADMIN"`

	TestingIDs      []int64       `long:"testing-id" env:"TESTING_ID" env-delim:"," description:"testing ids, allow bot to reply to them"`
	HistoryDuration time.Duration `long:"history-duration" env:"HISTORY_DURATION" default:"1h" description:"history duration"`

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

	Files struct {
		SamplesSpamFile  string `long:"samples-spam" env:"SAMPLES_SPAM" default:"data/spam-samples.txt" description:"path to spam samples"`
		SamplesHamFile   string `long:"samples-ham" env:"SAMPLES_HAM" default:"data/ham-samples.txt" description:"path to ham samples"`
		ExcludeTokenFile string `long:"exclude-tokens" env:"EXCLUDE_TOKENS" default:"data/exclude-tokens.txt" description:"path to exclude tokens file"`
		StopWordsFile    string `long:"stop-words" env:"STOP_WORDS" default:"data/stop-words.txt" description:"path to stop words file"`
		DynamicSpamFile  string `long:"dynamic-spam" env:"DYNAMIC_SPAM" default:"data/spam-dynamic.txt" description:"path to dynamic spam file"`
		DynamicHamFile   string `long:"dynamic-ham" env:"DYNAMIC_HAM" default:"data/ham-dynamic.txt" description:"path to dynamic ham file"`
	} `group:"files" namespace:"files" env-namespace:"FILES"`

	SimilarityThreshold float64 `long:"similarity-threshold" env:"SIMILARITY_THRESHOLD" default:"0.5" description:"spam threshold"`

	MinMsgLen    int  `long:"min-msg-len" env:"MIN_MSG_LEN" default:"50" description:"min message length to check"`
	MaxEmoji     int  `long:"max-emoji" env:"MAX_EMOJI" default:"2" description:"max emoji count in message"`
	ParanoidMode bool `long:"paranoid" env:"PARANOID" description:"paranoid mode, check all messages"`

	Message struct {
		Startup string `long:"startup" env:"STARTUP" default:"" description:"startup message"`
		Spam    string `long:"spam" env:"SPAM" default:"this is spam" description:"spam message"`
		Dry     string `long:"dry" env:"DRY" default:"this is spam (dry mode)" description:"spam dry message"`
	} `group:"message" namespace:"message" env-namespace:"MESSAGE"`

	Dry   bool `long:"dry" env:"DRY" description:"dry mode, no bans"`
	Dbg   bool `long:"dbg" env:"DEBUG" description:"debug mode"`
	TGDbg bool `long:"tg-dbg" env:"TG_DEBUG" description:"telegram debug mode"`
}

var revision = "local"

func main() {
	fmt.Printf("tg-spam %s\n", revision)

	p := flags.NewParser(&opts, flags.PrintErrors|flags.PassDoubleDash|flags.HelpFlag)
	p.SubcommandsOptional = true
	if _, err := p.Parse(); err != nil {
		if err.(*flags.Error).Type != flags.ErrHelp {
			log.Printf("[ERROR] cli error: %v", err)
		}
		os.Exit(2)
	}

	setupLog(opts.Dbg, opts.Telegram.Token, opts.Admin.Secret)
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

	if err := execute(ctx); err != nil {
		log.Printf("[ERROR] %v", err)
		os.Exit(1)
	}
}

func execute(ctx context.Context) error {
	if opts.Dry {
		log.Print("[WARN] dry mode, no actual bans")
	}

	tbAPI, err := tbapi.NewBotAPI(opts.Telegram.Token)
	if err != nil {
		return fmt.Errorf("can't make telegram bot, %w", err)
	}
	tbAPI.Debug = opts.TGDbg

	detector := lib.NewDetector(lib.Config{
		MaxAllowedEmoji:     opts.MaxEmoji,
		MinMsgLen:           opts.MinMsgLen,
		SimilarityThreshold: opts.SimilarityThreshold,
		CasAPI:              opts.CAS.API,
		HTTPClient:          &http.Client{Timeout: opts.CAS.Timeout},
		FirstMessageOnly:    !opts.ParanoidMode,
	})
	detector.WithSpamUpdater(bot.NewSampleUpdater(opts.Files.DynamicSpamFile))
	detector.WithHamUpdater(bot.NewSampleUpdater(opts.Files.DynamicHamFile))
	spamBot := bot.NewSpamFilter(ctx, detector, bot.SpamParams{
		SpamSamplesFile:    opts.Files.SamplesSpamFile,
		HamSamplesFile:     opts.Files.SamplesHamFile,
		SpamDynamicFile:    opts.Files.DynamicSpamFile,
		HamDynamicFile:     opts.Files.DynamicHamFile,
		ExcludedTokensFile: opts.Files.ExcludeTokenFile,
		StopWordsFile:      opts.Files.StopWordsFile,
		SpamMsg:            opts.Message.Spam,
		SpamDryMsg:         opts.Message.Dry,
		WatchDelay:         time.Second * 5,
		Dry:                opts.Dry,
	})

	if err = spamBot.ReloadSamples(); err != nil {
		return fmt.Errorf("can't make spam bot, %w", err)
	}

	web, err := server.NewSpamWeb(tbAPI, server.Params{
		TgGroup:    opts.Telegram.Group,
		URL:        opts.Admin.URL,
		Secret:     opts.Admin.Secret,
		ListenAddr: opts.Admin.Address,
	})
	if err != nil {
		return fmt.Errorf("can't make spam rest, %w", err)
	}

	loggerWr, err := makeSpamLogWriter()
	if err != nil {
		return fmt.Errorf("can't make spam log writer, %w", err)
	}
	defer loggerWr.Close()

	tgListener := events.TelegramListener{
		TbAPI:        tbAPI,
		Group:        opts.Telegram.Group,
		IdleDuration: opts.Telegram.IdleDuration,
		SuperUsers:   opts.SuperUsers,
		Bot:          spamBot,
		StartupMsg:   opts.Message.Startup,
		NoSpamReply:  opts.NoSpamReply,
		SpamLogger:   makeSpamLogger(loggerWr),
		AdminGroup:   opts.Admin.Group,
		TestingIDs:   opts.TestingIDs,
		SpamWeb:      web,
		Locator:      events.NewLocator(opts.HistoryDuration),
		Dry:          opts.Dry,
	}

	if opts.Admin.URL != "" && opts.Admin.Secret != "" {
		go func() {
			if err := web.Run(ctx); err != nil {
				log.Printf("[ERROR] admin web server failed, %v", err)
			}
		}()
	} else {
		log.Print("[WARN] admin web server is disabled")
	}

	if err := tgListener.Do(ctx); err != nil {
		return fmt.Errorf("telegram listener failed, %w", err)
	}
	return nil
}

func makeSpamLogger(wr io.Writer) events.SpamLogger {
	return events.SpamLoggerFunc(func(msg *bot.Message, response *bot.Response) {
		log.Printf("[INFO] spam detected from %v, response: %s", msg.From, response.Text)
		log.Printf("[DEBUG] spam message:  %q", msg.Text)
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
			Text:        strings.ReplaceAll(msg.Text, "\n", " "),
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

func makeSpamLogWriter() (accessLog io.WriteCloser, err error) {
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
