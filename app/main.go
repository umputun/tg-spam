package main

import "time"

var opts struct {
	Telegram struct {
		Token   string        `long:"token" env:"TOKEN" description:"telegram bot token" default:"test"`
		Group   string        `long:"group" env:"GROUP" description:"group name/id" default:"test"`
		Timeout time.Duration `long:"timeout" env:"TIMEOUT" description:"http client timeout for getting files from Telegram" default:"30s"`
	} `group:"telegram" namespace:"telegram" env-namespace:"TELEGRAM"`

	LogsPath     string        `short:"l" long:"logs" env:"TELEGRAM_LOGS" default:"logs" description:"path to logs"`
	SuperUsers   []string      `long:"super" description:"super-users"`
	IdleDuration time.Duration `long:"idle" env:"IDLE" default:"30s" description:"idle duration"`

	CasAPI              string        `long:"api" env:"CAS_API" default:"https://api.cas.chat" description:"CAS API"`
	CasTimeOut          time.Duration `long:"timeout" env:"TIMEOUT" default:"5s" description:"CAS timeout"`
	Samples             string        `long:"samples" env:"SAMPLES" default:"" description:"path to spam samples"`
	SimilarityThreshold float64       `long:"threshold" env:"THRESHOLD" default:"0.5" description:"spam threshold"`
	MinMsgLen           int           `long:"min-msg-len" env:"MIN_MSG_LEN" default:"100" description:"min message length to check"`
	MaxEmoji            int           `long:"max-emoji" env:"MAX_EMOJI" default:"5" description:"max emoji count in message"`
	StopWords           string        `long:"stop-words" env:"STOP_WORDS" default:"" description:"path to stop words file"`

	Dry bool `long:"dry" env:"DRY" description:"dry mode, no bans"`
	Dbg bool `long:"dbg" env:"DEBUG" description:"debug mode"`
}

var revision = "local"

func main() {

}
