# Database Configuration Support - Implementation

## Overview

This document describes the implemented database configuration support for the TG-Spam application. The feature allows storing and loading application configuration from a database, while maintaining backward compatibility with CLI-based configuration.

## Key Design Decisions

1. **Package Structure**: Created a dedicated `config` package (not `settings`) to avoid confusion with the existing webapi Settings struct
2. **Domain-Driven Design**: Configuration is organized by functional domains (Telegram, Admin, OpenAI, etc.)
3. **Security First**: Sensitive fields are encrypted using AES-256-GCM with Argon2 key derivation
4. **CLI Precedence**: CLI always owns connection/control flags and the web auth password/hash; database owns everything else in `--confdb` mode

## Implementation Summary

The implementation consists of four main components:

1. **Config Package** (`app/config/`):
   - `Settings` struct with domain-specific nested structures
   - `Store` for database persistence using JSON storage
   - `Crypter` for field-level encryption of sensitive data
   - Support for both SQLite and PostgreSQL

2. **CLI Integration**:
   - `save-config` command to persist current configuration
   - `--confdb` flag to enable database configuration mode
   - `--confdb-encrypt-key` flag for encryption key

3. **Web API Integration**:
   - Configuration management endpoints (save, load, update, delete)
   - HTMX-based UI updates
   - Proper authentication and authorization

4. **Security Features**:
   - Encryption for tokens and password hashes
   - Transient settings that are never persisted
   - CLI override for web auth password/hash even in `--confdb` mode (bootstrap recovery)

## Implementation Details

### 1. Config Package

The config package provides a clean domain model for application configuration:

```go
// app/config/settings.go
package config

// Settings represents application configuration independent of source (CLI, DB, etc)
type Settings struct {
    // Core settings
    InstanceID string `json:"instance_id" yaml:"instance_id" db:"instance_id"`

    // Group settings by domain
    Telegram      TelegramSettings      `json:"telegram" yaml:"telegram" db:"telegram"`
    Admin         AdminSettings         `json:"admin" yaml:"admin" db:"admin"`
    History       HistorySettings       `json:"history" yaml:"history" db:"history"`
    Logger        LoggerSettings        `json:"logger" yaml:"logger" db:"logger"`
    CAS           CASSettings           `json:"cas" yaml:"cas" db:"cas"`
    Meta          MetaSettings          `json:"meta" yaml:"meta" db:"meta"`
    OpenAI        OpenAISettings        `json:"openai" yaml:"openai" db:"openai"`
    Gemini        GeminiSettings        `json:"gemini" yaml:"gemini" db:"gemini"`
    LLM           LLMSettings           `json:"llm" yaml:"llm" db:"llm"`
    LuaPlugins    LuaPluginsSettings    `json:"lua_plugins" yaml:"lua_plugins" db:"lua_plugins"`
    AbnormalSpace AbnormalSpaceSettings `json:"abnormal_spacing" yaml:"abnormal_spacing" db:"abnormal_spacing"`
    Files         FilesSettings         `json:"files" yaml:"files" db:"files"`
    Message       MessageSettings       `json:"message" yaml:"message" db:"message"`
    Server        ServerSettings        `json:"server" yaml:"server" db:"server"`
    Delete        DeleteSettings        `json:"delete" yaml:"delete" db:"delete"`
    Duplicates    DuplicatesSettings    `json:"duplicates" yaml:"duplicates" db:"duplicates"`
    Report        ReportSettings        `json:"report" yaml:"report" db:"report"`

    // Spam detection settings
    SimilarityThreshold float64 `json:"similarity_threshold" yaml:"similarity_threshold" db:"similarity_threshold"`
    MinMsgLen           int     `json:"min_msg_len" yaml:"min_msg_len" db:"min_msg_len"`
    MaxEmoji            int     `json:"max_emoji" yaml:"max_emoji" db:"max_emoji"`
    MinSpamProbability  float64 `json:"min_spam_probability" yaml:"min_spam_probability" db:"min_spam_probability"`
    MultiLangWords      int     `json:"multi_lang_words" yaml:"multi_lang_words" db:"multi_lang_words"`

    // Cleanup settings
    AggressiveCleanup      bool `json:"aggressive_cleanup" yaml:"aggressive_cleanup" db:"aggressive_cleanup"`
    AggressiveCleanupLimit int  `json:"aggressive_cleanup_limit" yaml:"aggressive_cleanup_limit" db:"aggressive_cleanup_limit"`

    // Transient fields that should never be stored
    Transient TransientSettings `json:"-" yaml:"-"`
}

// TransientSettings contains settings that should never be persisted
type TransientSettings struct {
    DataBaseURL        string        `json:"-" yaml:"-"`
    StorageTimeout     time.Duration `json:"-" yaml:"-"`
    ConfigDB           bool          `json:"-" yaml:"-"`
    Dbg                bool          `json:"-" yaml:"-"`
    TGDbg              bool          `json:"-" yaml:"-"`
    ConfigDBEncryptKey string        `json:"-" yaml:"-"`
    WebAuthPasswd      string        `json:"-" yaml:"-"`
    AuthFromCLI        bool          `json:"-" yaml:"-"` // tracks whether auth came from --server.auth/-hash so startup logs reflect CLI override
}
```

### 2. Config Store

The store provides database persistence with encryption support:

```go
// app/config/store.go
package config

// Store provides access to settings stored in database
type Store struct {
    *engine.SQL
    engine.RWLocker
    crypter         *Crypter
    sensitiveFields []string
}

// NewStore creates a new settings store with options
func NewStore(ctx context.Context, db *engine.SQL, opts ...StoreOption) (*Store, error) {
    // Initialize with default sensitive fields
    // Apply options (like encryption)
    // Create database table if needed
}

// Load retrieves the settings from the database
func (s *Store) Load(ctx context.Context) (*Settings, error) {
    // Load JSON from database
    // Decrypt sensitive fields if crypter is configured
    // Return parsed settings
}

// Save stores the settings to the database
func (s *Store) Save(ctx context.Context, settings *Settings) error {
    // Create a safe copy without transient fields
    // Encrypt sensitive fields if crypter is configured
    // Save JSON to database
}
```

### 3. CLI Integration

The main.go integrates the config package seamlessly:

```go
var appSettings *config.Settings

if opts.ConfigDB {
    // database configuration mode - load from database first
    appSettings = config.New()

    // set transient values needed for database connection BEFORE loading
    appSettings.Transient.ConfigDB = opts.ConfigDB
    appSettings.Transient.ConfigDBEncryptKey = opts.ConfigDBEncryptKey
    appSettings.Transient.DataBaseURL = opts.DataBaseURL
    appSettings.InstanceID = opts.InstanceID
    appSettings.Files.DynamicDataPath = opts.Files.DynamicDataPath

    // load settings from database (overwrites the empty struct)
    if err := loadConfigFromDB(ctx, appSettings); err != nil {
        log.Printf("[ERROR] failed to load configuration from database: %v", err)
        os.Exit(1)
    }

    // apply remaining transient values from CLI (these are never stored in DB)
    appSettings.Transient.Dbg = opts.Dbg
    appSettings.Transient.TGDbg = opts.TGDbg

    // apply explicit CLI overrides for non-transient values (auth, tokens, dry)
    applyCLIOverrides(appSettings, opts)
} else {
    // traditional mode - CLI is source of truth
    appSettings = optToSettings(opts)
}

// Handle save-config command
if p.Active != nil && p.Active.Name == "save-config" {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    if err := saveConfigToDB(ctx, appSettings); err != nil {
        log.Printf("[ERROR] failed to save configuration to database: %v", err)
        os.Exit(1)
    }
    cancel()
    os.Exit(0)
}
```

In `--confdb` mode, CLI tokens (`Telegram.Token`, `OpenAI.Token`, `Gemini.Token`) are not
applied; the database is authoritative for them. Only auth password/hash are
reapplied via `applyCLIOverrides`, because the bootstrap path must let an operator
recover access if the stored hash is lost. The `save-config` command captures the
current CLI configuration into the DB so the subsequent `--confdb` start sees it.

### 4. Web API Integration

The web API provides full configuration management capabilities:

```go
// Configuration routes are added when ConfigDB mode is enabled
if s.SettingsStore != nil && s.ConfigDBMode {
    webUI.Route(func(config *routegroup.Bundle) {
        config.HandleFunc("POST /config", s.saveConfigHandler)        // Save to DB
        config.HandleFunc("POST /config/reload", s.loadConfigHandler) // Reload from DB (state-changing, non-safe method)
        config.HandleFunc("PUT /config", s.updateConfigHandler)       // Update settings
        config.HandleFunc("DELETE /config", s.deleteConfigHandler)    // Delete from DB
    })
}

// Settings are integrated throughout the server
type Server struct {
    Config
}

type Config struct {
    // ... other fields ...
    SettingsStore SettingsStore    // Interface for config persistence
    AppSettings   *config.Settings // The actual settings
    ConfigDBMode  bool            // Indicates DB config is enabled
}
```

## Integration Challenges and Solutions

### 1. Sensitive Information Handling

Sensitive information like API tokens and passwords needs proper handling. We've addressed this by:

- Storing credentials within their proper domain models (`Telegram.Token`, `OpenAI.Token`, `Gemini.Token`, `Server.AuthHash`)
- Encrypting each at rest with an `ENC:` prefix when an encryption key is configured
- Storing the temporary password used for hash generation in the `Transient` field with `json:"-"` and `yaml:"-"` tags
- Automatically creating a safe copy of settings before database storage (transient fields stripped)

### 2. CLI/DB Precedence

Settings resolve based on the run mode:

- Without `--confdb`: CLI is the sole source of truth; `optToSettings` converts the flag struct to `*config.Settings`
- With `--confdb`: the database is the source of truth for persisted fields; CLI-provided credentials (`--telegram.token`, `--openai.token`, `--gemini.token`), auth (`--server.auth`, `--server.auth-hash`), and `--dry` are overlaid on top via `applyCLIOverrides` so an operator can rotate secrets or toggle dry-run without touching the DB
- Always from CLI regardless of mode: `DataBaseURL`, `StorageTimeout`, `ConfigDB`, `ConfigDBEncryptKey`, `Dbg`, `TGDbg` (marked transient, never persisted)
- `Dry` is persisted in the DB and one-way-overridable from CLI: `--dry` forces true, CLI default (unset) preserves the DB value. To disable dry-run after enabling it, use the settings UI or `save-config`
- CLI override path in `--confdb` mode (handled by `applyCLIOverrides`):
  - web auth password (`--server.auth`) and web auth hash (`--server.auth-hash`) — override-only so an operator can recover UI access
  - API tokens `--telegram.token`, `--openai.token`, `--gemini.token` — non-empty CLI values overlay the DB; empty CLI values leave the DB-stored token in place

`Gemini.Token` follows the same precedence model as `OpenAI.Token`: CLI overlays DB via `applyCLIOverrides`, encrypted at rest with the same `ENC:` prefix scheme.

### 3. Backward Compatibility

To maintain backward compatibility, we:

- Keep the existing CLI parsing with the `options` struct
- Add conversion functions between `options` and our new `Settings` type
- Preserve the original behavior when `--confdb` is not specified

## Persisted Settings Inventory

All fields on `*config.Settings` other than `Transient` are persisted as a single
encrypted JSON blob. Sensitive string fields are encrypted individually with an
`ENC:` prefix; the rest are stored in plaintext inside the blob.

### Persisted Groups

- `InstanceID`
- `Telegram` — group, idle duration, timeout, token (encrypted)
- `Admin` — admin group, disable forward, testing IDs, super users
- `History` — duration, min size, size
- `Logger` — enabled, filename, max size, max backups
- `CAS` — API, timeout, user agent
- `Meta` — links limit, mentions limit, image/links/videos/audios-only, forward, keyboard, username symbols, **`contact_only`**, **`giveaway`**
- `OpenAI` — full config, token (encrypted)
- `Gemini` — token (encrypted), veto, prompt, custom prompts, model, max tokens response (`int32`), max symbols request, retry count, history size, check short messages
- `LLM` — consensus, request timeout
- `LuaPlugins` — enabled, plugins dir, enabled plugins list, dynamic reload
- `AbnormalSpace` — ratio thresholds, short-word parameters, min words
- `Files` — samples path, dynamic path, watch interval
- `Message` — startup, spam, dry, warn
- `Server` — enabled, listen address, auth hash (encrypted)
- `Delete` — **`join_messages`**, **`leave_messages`**
- `Duplicates` — threshold, window
- `Report` — enabled, threshold, auto-ban threshold, rate limit, rate period
- Top-level scalars: similarity/probability/emoji thresholds, `multi_lang_words`, `no_spam_reply`, `suppress_join_message`, **`aggressive_cleanup`**, **`aggressive_cleanup_limit`**, paranoid mode, first messages count, training/soft-ban/convert/max-backups/dry

### Schema Compatibility

The store writes a single JSON blob per row, wrapped by the Crypter for sensitive
fields. Adding new fields is strictly additive:

- Old blobs decode into newer `*config.Settings` with zero-value defaults on the
  new fields; no migration is needed.
- The next `Save` overwrites the entire blob, so any JSON keys present in storage
  that no longer exist on the struct are dropped on the next write.

### Migration Note for Early `--confdb` Users

The web auth username is configurable via the persisted `Server.AuthUser`
field (JSON key `auth_user`). When unset, `activeAuthUser()` falls back to
the startup `AuthUser` and finally to the `"tg-spam"` default, so blobs
written without this field continue to work unchanged.

A short-lived intermediate revision dropped the field entirely; if a blob
written during that window omits `auth_user`, the default `"tg-spam"`
username applies. Operators who relied on a custom username can restore it
through the settings UI or by re-running `save-config`. No action is
required for blobs that already carry the field.

## Usage

### Saving Configuration to Database

To save the current configuration to the database:

```bash
# Save current CLI configuration to database
./tg-spam save-config --db "postgres://user:pass@localhost/tgspam"

# Save with encryption enabled
./tg-spam save-config --db "postgres://user:pass@localhost/tgspam" --confdb-encrypt-key "my-secret-key-at-least-20-chars"
```

### Loading Configuration from Database

To run the application with database configuration:

```bash
# Load configuration from database
./tg-spam --confdb --db "postgres://user:pass@localhost/tgspam" \
    --telegram.token "BOT_TOKEN"

# With encryption
./tg-spam --confdb --db "postgres://user:pass@localhost/tgspam" \
    --confdb-encrypt-key "my-secret-key-at-least-20-chars" \
    --telegram.token "BOT_TOKEN"
```

Note: Security-sensitive parameters (tokens, passwords) must still be provided via CLI for security reasons.

### Web UI Configuration Management

When running with `--confdb` and `--server.enabled`, the web UI provides configuration management at:
- POST `/config` - Save current configuration to database
- POST `/config/reload` - Reload configuration from database (non-safe method so cross-origin CSRF protection applies)
- PUT `/config` - Update specific settings
- DELETE `/config` - Remove configuration from database

## Current Implementation Status

### Completed Components

1. **Config Package** (`app/config/`):
   - `Settings` struct with all configuration organized by domain
   - `Store` for database persistence with JSON serialization
   - `Crypter` for AES-256-GCM encryption of sensitive fields
   - Support for both SQLite and PostgreSQL databases
   - Comprehensive test coverage

2. **CLI Integration** (`app/main.go`):
   - `save-config` command for persisting configuration
   - `--confdb` flag to enable database configuration mode
   - `--confdb-encrypt-key` for encryption support
   - `optToSettings()` function for CLI to domain model conversion
   - `loadConfigFromDB()` and `saveConfigToDB()` functions
   - Proper precedence rules (CLI overrides DB for credentials)

3. **Web API Integration** (`app/webapi/`):
   - `SettingsStore` interface for configuration persistence
   - Four configuration management endpoints
   - HTMX integration for dynamic UI updates
   - Routes only enabled when in ConfigDB mode
   - Form-based updates with in-memory and DB persistence options

4. **Security Features**:
   - Field-level encryption for sensitive data (`Telegram.Token`, `OpenAI.Token`, `Gemini.Token`, `Server.AuthHash`)
   - Transient settings never persisted
   - Web auth password/hash overridable from CLI even in `--confdb` mode (bootstrap recovery)
   - Bcrypt hash generation for web authentication

### Implementation Details

- The config package was chosen over "settings" to avoid confusion with the existing webapi Settings struct
- Credentials are stored within their domain models (e.g., `Telegram.Token`, `OpenAI.Token`)
- The system maintains backward compatibility - works exactly as before when `--confdb` is not used
- All sensitive fields are automatically encrypted when an encryption key is provided
- The implementation uses JSON for storage, making it human-readable when not encrypted

### Test Coverage

All components have been tested:
- Config package tests pass for both SQLite and PostgreSQL
- Encryption/decryption functionality verified
- Web API handlers tested
- Store operations (CRUD) fully covered

The feature is complete and ready for production use.