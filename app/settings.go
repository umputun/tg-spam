package main

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"strconv"
	"time"

	"github.com/umputun/tg-spam/app/config"
)

// optToSettings converts CLI options to the domain settings model
func optToSettings(opts options) *config.Settings {
	// create settings model from options
	settings := &config.Settings{
		InstanceID: opts.InstanceID,

		Telegram: config.TelegramSettings{
			Group:        opts.Telegram.Group,
			IdleDuration: opts.Telegram.IdleDuration,
			Timeout:      opts.Telegram.Timeout,
		},

		Admin: config.AdminSettings{
			AdminGroup:              opts.AdminGroup,
			DisableAdminSpamForward: opts.DisableAdminSpamForward,
			TestingIDs:              opts.TestingIDs,
			SuperUsers:              opts.SuperUsers,
		},

		History: config.HistorySettings{
			Duration: opts.HistoryDuration,
			MinSize:  opts.HistoryMinSize,
			Size:     opts.HistorySize,
		},

		Logger: config.LoggerSettings{
			Enabled:    opts.Logger.Enabled,
			FileName:   opts.Logger.FileName,
			MaxSize:    opts.Logger.MaxSize,
			MaxBackups: opts.Logger.MaxBackups,
		},

		CAS: config.CASSettings{
			API:       opts.CAS.API,
			Timeout:   opts.CAS.Timeout,
			UserAgent: opts.CAS.UserAgent,
		},

		Meta: config.MetaSettings{
			LinksLimit:      opts.Meta.LinksLimit,
			MentionsLimit:   opts.Meta.MentionsLimit,
			ImageOnly:       opts.Meta.ImageOnly,
			LinksOnly:       opts.Meta.LinksOnly,
			VideosOnly:      opts.Meta.VideosOnly,
			AudiosOnly:      opts.Meta.AudiosOnly,
			Forward:         opts.Meta.Forward,
			Keyboard:        opts.Meta.Keyboard,
			UsernameSymbols: opts.Meta.UsernameSymbols,
			ContactOnly:     opts.Meta.ContactOnly,
			Giveaway:        opts.Meta.Giveaway,
		},

		OpenAI: config.OpenAISettings{
			APIBase:            opts.OpenAI.APIBase,
			Veto:               opts.OpenAI.Veto,
			Prompt:             opts.OpenAI.Prompt,
			CustomPrompts:      opts.OpenAI.CustomPrompts,
			Model:              opts.OpenAI.Model,
			MaxTokensResponse:  opts.OpenAI.MaxTokensResponse,
			MaxTokensRequest:   opts.OpenAI.MaxTokensRequest,
			MaxSymbolsRequest:  opts.OpenAI.MaxSymbolsRequest,
			RetryCount:         opts.OpenAI.RetryCount,
			HistorySize:        opts.OpenAI.HistorySize,
			ReasoningEffort:    opts.OpenAI.ReasoningEffort,
			CheckShortMessages: opts.OpenAI.CheckShortMessages,
		},

		Gemini: config.GeminiSettings{
			Veto:               opts.Gemini.Veto,
			Prompt:             opts.Gemini.Prompt,
			CustomPrompts:      opts.Gemini.CustomPrompts,
			Model:              opts.Gemini.Model,
			MaxTokensResponse:  opts.Gemini.MaxTokensResponse,
			MaxSymbolsRequest:  opts.Gemini.MaxSymbolsRequest,
			RetryCount:         opts.Gemini.RetryCount,
			HistorySize:        opts.Gemini.HistorySize,
			CheckShortMessages: opts.Gemini.CheckShortMessages,
		},

		LLM: config.LLMSettings{
			Consensus:      opts.LLM.Consensus,
			RequestTimeout: opts.LLM.RequestTimeout,
		},

		Delete: config.DeleteSettings{
			JoinMessages:  opts.Delete.JoinMessages,
			LeaveMessages: opts.Delete.LeaveMessages,
		},

		Duplicates: config.DuplicatesSettings{
			Threshold: opts.Duplicates.Threshold,
			Window:    opts.Duplicates.Window,
		},

		Reactions: config.ReactionsSettings{
			MaxReactions: opts.Reactions.MaxReactions,
			Window:       opts.Reactions.Window,
		},

		Report: config.ReportSettings{
			Enabled:          opts.Report.Enabled,
			Threshold:        opts.Report.Threshold,
			AutoBanThreshold: opts.Report.AutoBanThreshold,
			RateLimit:        opts.Report.RateLimit,
			RatePeriod:       opts.Report.RatePeriod,
		},

		LuaPlugins: config.LuaPluginsSettings{
			Enabled:        opts.LuaPlugins.Enabled,
			PluginsDir:     opts.LuaPlugins.PluginsDir,
			EnabledPlugins: opts.LuaPlugins.EnabledPlugins,
			DynamicReload:  opts.LuaPlugins.DynamicReload,
		},

		AbnormalSpace: config.AbnormalSpaceSettings{
			Enabled:                 opts.AbnormalSpacing.Enabled,
			SpaceRatioThreshold:     opts.AbnormalSpacing.SpaceRatioThreshold,
			ShortWordRatioThreshold: opts.AbnormalSpacing.ShortWordRatioThreshold,
			ShortWordLen:            opts.AbnormalSpacing.ShortWordLen,
			MinWords:                opts.AbnormalSpacing.MinWords,
		},

		Files: config.FilesSettings{
			SamplesDataPath: opts.Files.SamplesDataPath,
			DynamicDataPath: opts.Files.DynamicDataPath,
			WatchInterval:   int(opts.Files.WatchInterval.Seconds()),
		},

		Message: config.MessageSettings{
			Startup: opts.Message.Startup,
			Spam:    opts.Message.Spam,
			Dry:     opts.Message.Dry,
			Warn:    opts.Message.Warn,
		},

		Server: config.ServerSettings{
			Enabled:    opts.Server.Enabled,
			ListenAddr: opts.Server.ListenAddr,
		},

		SimilarityThreshold:    opts.SimilarityThreshold,
		MinMsgLen:              opts.MinMsgLen,
		MaxEmoji:               opts.MaxEmoji,
		MinSpamProbability:     opts.MinSpamProbability,
		MultiLangWords:         opts.MultiLangWords,
		NoSpamReply:            opts.NoSpamReply,
		SuppressJoinMessage:    opts.SuppressJoinMessage,
		AggressiveCleanup:      opts.AggressiveCleanup,
		AggressiveCleanupLimit: opts.AggressiveCleanupLimit,
		ParanoidMode:           opts.ParanoidMode,
		FirstMessagesCount:     opts.FirstMessagesCount,
		Training:               opts.Training,
		SoftBan:                opts.SoftBan,
		Convert:                opts.Convert,
		MaxBackups:             opts.MaxBackups,
		Dry:                    opts.Dry,
	}

	// set transient settings (not persisted to database)
	settings.Transient = config.TransientSettings{
		DataBaseURL:        opts.DataBaseURL,
		StorageTimeout:     opts.StorageTimeout,
		ConfigDB:           opts.ConfigDB,
		Dbg:                opts.Dbg,
		TGDbg:              opts.TGDbg,
		WebAuthPasswd:      opts.Server.AuthPasswd,
		ConfigDBEncryptKey: opts.ConfigDBEncryptKey,
	}

	// set credentials in their respective domain structures
	settings.Telegram.Token = opts.Telegram.Token
	settings.OpenAI.Token = opts.OpenAI.Token
	settings.Gemini.Token = opts.Gemini.Token
	settings.Server.AuthHash = opts.Server.AuthHash

	return settings
}

// defaultSettingsTemplate builds a *config.Settings populated with the canonical
// CLI defaults expressed as `default:` struct tags on the options type. Used as
// a single source of truth by both applyCLIOverrides (for "is this value the
// CLI default?" comparisons) and (*config.Settings).ApplyDefaults (for filling
// missing keys in DB-loaded blobs).
//
// Pure reflection is used instead of go-flags.Parse([]string{}) on purpose:
// go-flags reads os.LookupEnv during default-fill, which would let ambient
// environment values like SERVER_LISTEN leak into the template and silently
// break both override comparisons and defaults-fill semantics.
func defaultSettingsTemplate() (*config.Settings, error) {
	var opts options
	if err := applyStructTagDefaults(reflect.ValueOf(&opts).Elem()); err != nil {
		return nil, fmt.Errorf("failed to derive defaults from struct tags: %w", err)
	}
	return optToSettings(opts), nil
}

// durationType is reused by applyStructTagDefaults to identify time.Duration
// fields, whose reflect.Kind is Int64 but which require time.ParseDuration.
var durationType = reflect.TypeFor[time.Duration]()

// applyStructTagDefaults walks v recursively, parses each leaf field's
// `default:"..."` struct tag, and assigns a typed value via the dispatcher
// below. Nested struct fields are recursed into; fields without a default tag
// keep their Go zero value (matching CLI behavior when the flag is omitted
// and no env var is set).
//
// Returns an error for any field type the dispatcher does not handle, so a
// future contributor adding a new kind (e.g., uint) gets caught by tests
// rather than producing a silently-wrong template.
func applyStructTagDefaults(v reflect.Value) error {
	if v.Kind() != reflect.Struct {
		return nil
	}
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		ft := t.Field(i)
		if !ft.IsExported() {
			continue
		}
		f := v.Field(i)
		// recurse into plain nested structs; time.Duration has Kind=Int64 so it
		// falls through to the leaf branch below
		if f.Kind() == reflect.Struct && f.Type() != durationType {
			if err := applyStructTagDefaults(f); err != nil {
				return fmt.Errorf("%s: %w", ft.Name, err)
			}
			continue
		}
		tag, ok := ft.Tag.Lookup("default")
		if !ok {
			continue
		}
		if !f.CanSet() {
			continue
		}
		if err := setFieldFromDefaultTag(f, tag); err != nil {
			return fmt.Errorf("%s: %w", ft.Name, err)
		}
	}
	return nil
}

// setFieldFromDefaultTag parses tag according to f's type and assigns the
// resulting typed value to f. Supports the field types currently used by the
// options struct: string, bool, int family, uint family, float family, and
// time.Duration. Slice/map/pointer fields are not used with default tags in
// the options struct and will return an error if added without dispatcher
// support.
func setFieldFromDefaultTag(f reflect.Value, tag string) error {
	// time.Duration (Kind=Int64) must be checked before the generic int branch
	if f.Type() == durationType {
		d, err := time.ParseDuration(tag)
		if err != nil {
			return fmt.Errorf("invalid duration default %q: %w", tag, err)
		}
		f.SetInt(int64(d))
		return nil
	}
	switch f.Kind() {
	case reflect.String:
		f.SetString(tag)
	case reflect.Bool:
		b, err := strconv.ParseBool(tag)
		if err != nil {
			return fmt.Errorf("invalid bool default %q: %w", tag, err)
		}
		f.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := strconv.ParseInt(tag, 10, f.Type().Bits())
		if err != nil {
			return fmt.Errorf("invalid int default %q: %w", tag, err)
		}
		f.SetInt(i)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := strconv.ParseUint(tag, 10, f.Type().Bits())
		if err != nil {
			return fmt.Errorf("invalid uint default %q: %w", tag, err)
		}
		f.SetUint(u)
	case reflect.Float32, reflect.Float64:
		fl, err := strconv.ParseFloat(tag, f.Type().Bits())
		if err != nil {
			return fmt.Errorf("invalid float default %q: %w", tag, err)
		}
		f.SetFloat(fl)
	default:
		return fmt.Errorf("unsupported field kind %s for default tag %q", f.Kind(), tag)
	}
	return nil
}

// applyCLIOverrides applies explicit CLI overrides to settings loaded from database.
// Only overrides values that differ from their zero/default values — the DB remains
// the source of truth for any field the operator did not explicitly pass on the CLI.
//
// The defaults template is used to distinguish "operator passed the default value
// explicitly" from "operator did not pass the flag at all" for fields that have a
// `default:` struct tag. For fields without a default tag, an empty/zero CLI value
// is treated as "not passed".
//
// How to add new overrides:
//  1. Check if the CLI option was explicitly provided (not using default value)
//  2. Compare with the default value from the template (or "" / zero for fields
//     without a default tag)
//  3. Apply the override only if the value differs from the default
func applyCLIOverrides(settings *config.Settings, opts options, defaults *config.Settings) {
	// override credentials if provided on CLI. This lets an operator rotate tokens
	// without touching the DB — empty CLI value leaves the DB-stored token in place.
	if opts.Telegram.Token != "" {
		settings.Telegram.Token = opts.Telegram.Token
	}
	if opts.OpenAI.Token != "" {
		settings.OpenAI.Token = opts.OpenAI.Token
	}
	if opts.Gemini.Token != "" {
		settings.Gemini.Token = opts.Gemini.Token
	}

	// override auth password if explicitly provided (not using default "auto")
	if opts.Server.AuthPasswd != "auto" {
		settings.Transient.WebAuthPasswd = opts.Server.AuthPasswd
		settings.Transient.AuthFromCLI = true
		// clear auth hash since we have a new password
		settings.Server.AuthHash = ""
	}

	// override auth hash if explicitly provided
	if opts.Server.AuthHash != "" {
		settings.Server.AuthHash = opts.Server.AuthHash
		settings.Transient.AuthFromCLI = true
		// clear password since hash takes precedence
		settings.Transient.WebAuthPasswd = ""
	}

	// operational CLI overrides (dry-run, listen addr, file paths) live in a
	// separate helper because they must also be reapplied after POST
	// /config/reload — credentials/auth above intentionally are not reapplied
	// (DB rotation wins on reload), so the split keeps reload semantics narrow.
	applyOperationalCLIOverrides(settings, opts, defaults)
}

// applyOperationalCLIOverrides reapplies the subset of CLI overrides that
// must survive POST /config/reload: dry-run, listen address, dynamic and
// samples data paths. These are operational knobs an operator chose at
// startup and the DB's persisted value should never silently override them
// just because the operator clicked Reload.
func applyOperationalCLIOverrides(settings *config.Settings, opts options, defaults *config.Settings) {
	// override dry-run if explicitly enabled via CLI (default is false).
	// false never overrides the DB value; to disable dry-run after enabling it,
	// use the settings UI or save-config.
	if opts.Dry {
		settings.Dry = true
	}

	// override server listen address if operator passed a non-default value;
	// preserves DB value when CLI is left at the ":8080" default
	if opts.Server.ListenAddr != defaults.Server.ListenAddr {
		settings.Server.ListenAddr = opts.Server.ListenAddr
	}

	// override dynamic data path if operator passed a non-default value;
	// preserves DB value when CLI is left at the "data" default
	if opts.Files.DynamicDataPath != defaults.Files.DynamicDataPath {
		settings.Files.DynamicDataPath = opts.Files.DynamicDataPath
	}

	// override samples data path when operator passes a non-empty value;
	// SamplesDataPath has no default tag — empty means "use DynamicDataPath",
	// so empty CLI never overrides the DB value
	if opts.Files.SamplesDataPath != "" {
		settings.Files.SamplesDataPath = opts.Files.SamplesDataPath
	}
}

// applyAutoAuthFallback enables auto-generated password mode as a safety net
// when --confdb leaves the web UI without any auth material. It only fires
// when the server is enabled and no hash/password is present anywhere AND
// the operator did not deliberately opt out via --server.auth=. The
// !AuthFromCLI guard preserves the explicit opt-out semantics set by
// applyCLIOverrides when the operator passes any --server.auth= value other
// than "auto" (including empty, which disables auth on purpose).
//
// Without this fallback, a fresh DB row missing AuthHash would silently
// expose the web UI without authentication. With it, the legacy CLI behavior
// (default --server.auth=auto generates a random password) is preserved
// even when settings come from the database.
//
// Setting AuthFromCLI=true is load-bearing: the hash that activateServer
// later derives from "auto" is held only in memory (the DB row is empty by
// definition when the fallback fires). loadConfigHandler must then preserve
// the in-memory AuthHash across /config/reload, otherwise the next reload
// would silently drop the generated hash and a subsequent /config save
// would persist an empty hash to the DB.
func applyAutoAuthFallback(settings *config.Settings) {
	if settings.Server.Enabled &&
		settings.Server.AuthHash == "" &&
		settings.Transient.WebAuthPasswd == "" &&
		!settings.Transient.AuthFromCLI {
		log.Print("[WARN] no auth configured (DB empty, no CLI override) — generating random password")
		settings.Transient.WebAuthPasswd = "auto"
		settings.Transient.AuthFromCLI = true
	}
}

// loadConfigFromDB loads configuration from the database. Any field that was
// absent in the persisted JSON blob (and therefore loaded as Go zero) is filled
// from defaults, so legacy or partial blobs still yield a fully-populated
// settings value. Fields whose zero is a meaningful operator choice
// (zeroAwarePaths in the config package) are preserved regardless.
//
// Passing a nil defaults template disables the fill step — used by a few tests
// that care only about round-trip behavior.
func loadConfigFromDB(ctx context.Context, settings, defaults *config.Settings) error {
	log.Print("[INFO] loading configuration from database")

	// create database connection using the same logic as main data DB
	db, err := makeDB(ctx, settings)
	if err != nil {
		return fmt.Errorf("failed to connect to database for config: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			log.Printf("[WARN] failed to close config database: %v", closeErr)
		}
	}()

	// create settings store with encryption if key provided
	var storeOpts []config.StoreOption
	if settings.Transient.ConfigDBEncryptKey != "" {
		crypter, cryptErr := config.NewCrypter(settings.Transient.ConfigDBEncryptKey, settings.InstanceID)
		if cryptErr != nil {
			return fmt.Errorf("invalid encryption key: %w", cryptErr)
		}
		storeOpts = append(storeOpts, config.WithCrypter(crypter))
		log.Print("[INFO] configuration encryption enabled for database access")
	}

	settingsStore, err := config.NewStore(ctx, db, storeOpts...)
	if err != nil {
		return fmt.Errorf("failed to create settings store: %w", err)
	}

	// load settings
	dbSettings, err := settingsStore.Load(ctx)
	if err != nil {
		return fmt.Errorf("failed to load settings from database: %w", err)
	}

	// save original transient values only (non-functional values)
	transient := settings.Transient

	// replace settings with loaded values including credentials
	*settings = *dbSettings

	// restore transient values
	settings.Transient = transient

	// fill any field left zero by a partial/legacy blob from the CLI-default template
	settings.ApplyDefaults(defaults)

	log.Printf("[INFO] configuration loaded from database successfully")
	return nil
}

// saveConfigToDB saves the current configuration to the database
func saveConfigToDB(ctx context.Context, settings *config.Settings) error {
	log.Print("[INFO] saving configuration to database")

	// create database connection
	db, err := makeDB(ctx, settings)
	if err != nil {
		return fmt.Errorf("failed to connect to database for config: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			log.Printf("[WARN] failed to close config database: %v", closeErr)
		}
	}()

	// create settings store with encryption if key provided
	var storeOpts []config.StoreOption
	if settings.Transient.ConfigDBEncryptKey != "" {
		crypter, cryptErr := config.NewCrypter(settings.Transient.ConfigDBEncryptKey, settings.InstanceID)
		if cryptErr != nil {
			return fmt.Errorf("invalid encryption key: %w", cryptErr)
		}
		storeOpts = append(storeOpts, config.WithCrypter(crypter))
		log.Print("[INFO] configuration encryption enabled for database storage")
	}

	settingsStore, err := config.NewStore(ctx, db, storeOpts...)
	if err != nil {
		return fmt.Errorf("failed to create settings store: %w", err)
	}

	// generate auth hash if password is provided but hash isn't
	if settings.Transient.WebAuthPasswd != "" && settings.Server.AuthHash == "" {
		// generate bcrypt hash from the password
		hash, hashErr := generateAuthHash(settings.Transient.WebAuthPasswd)
		if hashErr != nil {
			return fmt.Errorf("failed to generate auth hash: %w", hashErr)
		}

		// update the hash directly in the Server settings domain
		settings.Server.AuthHash = hash
	}

	// save settings to database
	if err := settingsStore.Save(ctx, settings); err != nil {
		return fmt.Errorf("failed to save configuration to database: %w", err)
	}

	log.Printf("[INFO] configuration saved to database successfully")
	return nil
}
