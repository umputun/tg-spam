package tgbotapi

import (
	"context"
	"errors"
	stdlog "log"
	"log/slog"
	"os"
	"strings"
)

// BotLogger is an interface that represents the required methods to log data.
//
// Instead of requiring the standard logger, we can just specify the methods we
// use and allow users to pass anything that implements these.
type BotLogger interface {
	Println(v ...any)
	Printf(format string, v ...any)
}

var log BotLogger = stdlog.New(os.Stderr, "", stdlog.LstdFlags)

// SetLogger specifies the logger that the package should use.
//
// Deprecated: Use [NewBotAPIWithOptions] with [WithLogger] instead.
// [SetLogger] will be no-operation later and removed in future.
func SetLogger(logger BotLogger) error {
	if logger == nil {
		return errors.New("logger is nil")
	}
	log = logger
	return nil
}

func (bot *BotAPI) debugLoggingEnabled() bool {
	return bot.Debug && !bot.loggingDisabled
}

func (bot *BotAPI) logDebug(ctx context.Context, msg string, attrs ...slog.Attr) {
	if !bot.debugLoggingEnabled() {
		return
	}
	bot.logMessage(ctx, slog.LevelDebug, msg, attrs...)
}

func (bot *BotAPI) logRequestDebug(ctx context.Context, endpoint string, debugInfo requestDebug) {
	if !bot.debugLoggingEnabled() {
		return
	}
	switch logger := bot.logger.(type) {
	case BotLogger:
		if debugInfo.fileCount > 0 {
			logger.Printf("[DEBUG] Endpoint: %s, params: %v, with %d files", endpoint, debugInfo.params, debugInfo.fileCount)
			return
		}
		logger.Printf("[DEBUG] Endpoint: %s, params: %v", endpoint, debugInfo.params)
	case *slog.Logger:
		args := make([]any, 0, 6)
		args = append(args,
			"endpoint", endpoint,
			"params", debugInfo.params,
		)
		if debugInfo.fileCount > 0 {
			args = append(args, "file_count", debugInfo.fileCount)
		}
		logger.DebugContext(ctx, "telegram request", args...)
	default:
		if debugInfo.fileCount > 0 {
			log.Printf("[DEBUG] Endpoint: %s, params: %v, with %d files", endpoint, debugInfo.params, debugInfo.fileCount)
			return
		}
		log.Printf("[DEBUG] Endpoint: %s, params: %v", endpoint, debugInfo.params)
	}
}

func (bot *BotAPI) logResponseDebug(ctx context.Context, endpoint string, response string) {
	if !bot.debugLoggingEnabled() {
		return
	}
	switch logger := bot.logger.(type) {
	case BotLogger:
		logger.Printf("[DEBUG] Endpoint: %s, response: %s", endpoint, response)
	case *slog.Logger:
		logger.DebugContext(ctx, "telegram response",
			"endpoint", endpoint,
			"response", response,
		)
	default:
		log.Printf("[DEBUG] Endpoint: %s, response: %s", endpoint, response)
	}
}

func (bot *BotAPI) logUpdateError(ctx context.Context, err error) {
	if bot.loggingDisabled {
		return
	}
	switch logger := bot.logger.(type) {
	case BotLogger:
		logger.Printf("[DEBUG] Failed to get updates (%s), retrying in 3 seconds...", err)
	case *slog.Logger:
		logger.ErrorContext(ctx, "telegram get updates failed",
			"error", err,
		)
		logger.InfoContext(ctx, "telegram get updates retry scheduled",
			"delay", "3s",
		)
	default:
		log.Printf("[DEBUG] Failed to get updates (%s), retrying in 3 seconds...", err)
	}
}

func (bot *BotAPI) logMessage(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr) {
	switch logger := bot.logger.(type) {
	case BotLogger:
		if len(attrs) == 0 {
			logger.Printf("[%s] %s", level.String(), msg)
			return
		}

		args := make([]string, 0, len(attrs))
		for _, attr := range attrs {
			args = append(args, attr.String())
		}

		logger.Printf("[%s] %s (%s)", level.String(), msg, strings.Join(args, " "))
	case *slog.Logger:
		logger.LogAttrs(ctx, level, msg, attrs...)
	default:
		if len(attrs) == 0 {
			log.Printf("[%s] %s", level.String(), msg)
			return
		}

		args := make([]string, 0, len(attrs))
		for _, attr := range attrs {
			args = append(args, attr.String())
		}

		log.Printf("[%s] %s (%s)", level.String(), msg, strings.Join(args, " "))
	}
}
