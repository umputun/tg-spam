package tgbotapi

import (
	"fmt"
	"log/slog"
	"net/http"
)

type botAPIConfig struct {
	apiEndpoint     string
	fileEndpoint    string
	client          HTTPClient
	debug           bool
	buffer          int
	logger          any
	loggingDisabled bool
}

// BotAPIOption configures a BotAPI instance created by NewBotAPIWithOptions.
type BotAPIOption func(*botAPIConfig) error

func defaultBotAPIConfig() botAPIConfig {
	return botAPIConfig{
		apiEndpoint:  APIEndpoint,
		fileEndpoint: FileEndpoint,
		client:       &http.Client{},
		buffer:       100,
	}
}

// WithAPIEndpoint configures the Telegram Bot API endpoint.
func WithAPIEndpoint(apiEndpoint string) BotAPIOption {
	return func(config *botAPIConfig) error {
		config.apiEndpoint = apiEndpoint
		return nil
	}
}

// WithFileEndpoint configures the Telegram file download endpoint.
func WithFileEndpoint(fileEndpoint string) BotAPIOption {
	return func(config *botAPIConfig) error {
		config.fileEndpoint = fileEndpoint
		return nil
	}
}

// WithHTTPClient configures the HTTP client used for API requests.
func WithHTTPClient(client HTTPClient) BotAPIOption {
	return func(config *botAPIConfig) error {
		config.client = client
		return nil
	}
}

// WithDebug configures debug logging for API requests and responses.
func WithDebug(debug bool) BotAPIOption {
	return func(config *botAPIConfig) error {
		config.debug = debug
		return nil
	}
}

// WithUpdatesBuffer configures the update channel buffer capacity.
func WithUpdatesBuffer(capacity int) BotAPIOption {
	return func(config *botAPIConfig) error {
		config.buffer = capacity
		return nil
	}
}

// WithLogger configures a logger for this BotAPI instance.
// Logger should be one of [BotLogger] or [*slog.Logger].
func WithLogger(logger any) BotAPIOption {
	return func(config *botAPIConfig) error {
		switch logger.(type) {
		case nil:
			config.logger = nil
			config.loggingDisabled = true
		case BotLogger, *slog.Logger:
			config.logger = logger
			config.loggingDisabled = false
		default:
			return fmt.Errorf("invalid type (%T) of logger", logger)
		}
		return nil
	}
}

// WithLoggingDisabled disables all logging for this BotAPI instance.
func WithLoggingDisabled() BotAPIOption {
	return func(config *botAPIConfig) error {
		config.logger = nil
		config.loggingDisabled = true
		return nil
	}
}
