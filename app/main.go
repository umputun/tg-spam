package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	log "github.com/go-pkgz/lgr"
	tbapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/umputun/go-flags"

	"github.com/umputun/tg-spam/app/bot"
	"github.com/umputun/tg-spam/app/events"
)

var opts struct {
	Telegram struct {
		Token        string        `long:"token" env:"TOKEN" description:"telegram bot token" default:"test"`
		Group        string        `long:"group" env:"GROUP" description:"group name/id" default:"test"`
		Timeout      time.Duration `long:"timeout" env:"TIMEOUT" default:"30s" description:"http client timeout for telegram" `
		IdleDuration time.Duration `long:"idle" env:"IDLE" default:"30s" description:"idle duration"`
	} `group:"telegram" namespace:"telegram" env-namespace:"TELEGRAM"`

	LogsPath   string           `short:"l" long:"logs" env:"TELEGRAM_LOGS" default:"logs" description:"path to logs"`
	SuperUsers events.SuperUser `long:"super" description:"super-users"`

	CAS struct {
		API     string        `long:"api" env:"API" default:"https://api.cas.chat" description:"CAS API"`
		Timeout time.Duration `long:"timeout" env:"TIMEOUT" default:"5s" description:"CAS timeout"`
	} `group:"cas" namespace:"cas" env-namespace:"CAS"`

	Files struct {
		SamplesSpamFile  string `long:"samples-spam" env:"SAMPLES_SPAM" default:"spam-samples.txt" description:"path to spam samples"`
		SamplesHamFile   string `long:"samples-ham" env:"SAMPLES_HAM" default:"ham-samples.txt" description:"path to ham samples"`
		ExcludeTokenFile string `long:"exclude-tokens" env:"EXCLUDE_TOKENS" default:"exclude-tokens.txt" description:"path to exclude tokens file"`
		StopWordsFile    string `long:"stop-words" env:"STOP_WORDS" default:"stop-words.txt" description:"path to stop words file"`
	} `group:"files" namespace:"files" env-namespace:"FILES"`

	SimilarityThreshold float64 `long:"similarity-threshold" env:"SIMILARITY_THRESHOLD" default:"0.5" description:"spam threshold"`

	MinMsgLen    int  `long:"min-msg-len" env:"MIN_MSG_LEN" default:"50" description:"min message length to check"`
	MaxEmoji     int  `long:"max-emoji" env:"MAX_EMOJI" default:"5" description:"max emoji count in message"`
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

	setupLog(opts.Dbg, opts.Telegram.Token)
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
	tbAPI, err := tbapi.NewBotAPI(opts.Telegram.Token)
	if err != nil {
		return fmt.Errorf("can't make telegram bot, %w", err)
	}
	tbAPI.Debug = opts.Dbg

	spamBot, err := bot.NewSpamFilter(ctx, bot.SpamParams{
		SpamSamplesFile:     opts.Files.SamplesSpamFile,
		HamSamplesFile:      opts.Files.SamplesHamFile,
		ExcludedTokensFile:  opts.Files.ExcludeTokenFile,
		StopWordsFile:       opts.Files.StopWordsFile,
		SimilarityThreshold: opts.SimilarityThreshold,
		MaxAllowedEmoji:     opts.MaxEmoji,
		MinMsgLen:           opts.MinMsgLen,
		HTTPClient:          &http.Client{Timeout: 5 * time.Second},
		CasAPI:              opts.CAS.API,
		SpamMsg:             opts.Message.Spam,
		SpamDryMsg:          opts.Message.Dry,
		ParanoidMode:        opts.ParanoidMode,
		Dry:                 opts.Dry,
	})
	if err != nil {
		return fmt.Errorf("can't make spam bot, %w", err)
	}

	tgListener := events.TelegramListener{
		TbAPI:        tbAPI,
		Group:        opts.Telegram.Group,
		Debug:        opts.TGDbg,
		IdleDuration: opts.Telegram.IdleDuration,
		SuperUsers:   opts.SuperUsers,
		Bot:          spamBot,
		StartupMsg:   opts.Message.Startup,
		SpamLogger: events.SpamLoggerFunc(func(msg *bot.Message, response *bot.Response) {
			log.Printf("[INFO] spam detected from %v, response: %s", msg.From, response.Text)
			log.Printf("[DEBUG] spam message:  %q", msg.Text)
		}),
	}

	if err := tgListener.Do(ctx); err != nil {
		return fmt.Errorf("telegram listener failed, %w", err)
	}
	return nil
}

func setupLog(dbg bool, secrets ...string) {
	if dbg {
		log.Setup(log.Debug, log.CallerFile, log.CallerFunc, log.Msec, log.LevelBraces, log.Secret(secrets...))
		return
	}
	log.Setup(log.Msec, log.LevelBraces, log.Secret(secrets...))
}
