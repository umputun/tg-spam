# Database Configuration Support - Implementation

## Overview

This document describes the implemented database configuration support for the TG-Spam application. The feature allows storing and loading application configuration from a database, while maintaining backward compatibility with CLI-based configuration.

## Key Design Decisions

1. **Package Structure**: Created a dedicated `config` package (not `settings`) to avoid confusion with the existing webapi Settings struct
2. **Domain-Driven Design**: Configuration is organized by functional domains (Telegram, Admin, OpenAI, etc.)
3. **Security First**: Sensitive fields are encrypted using AES-256-GCM with Argon2 key derivation
4. **CLI Precedence**: CLI parameters always override database values for security-critical settings

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
   - Encryption for tokens and passwords
   - Transient settings that are never persisted
   - CLI credentials take precedence over database values

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
    LuaPlugins    LuaPluginsSettings    `json:"lua_plugins" yaml:"lua_plugins" db:"lua_plugins"`
    AbnormalSpace AbnormalSpaceSettings `json:"abnormal_spacing" yaml:"abnormal_spacing" db:"abnormal_spacing"`
    Files         FilesSettings         `json:"files" yaml:"files" db:"files"`
    Message       MessageSettings       `json:"message" yaml:"message" db:"message"`
    Server        ServerSettings        `json:"server" yaml:"server" db:"server"`

    // Spam detection settings
    SimilarityThreshold float64 `json:"similarity_threshold" yaml:"similarity_threshold" db:"similarity_threshold"`
    MinMsgLen           int     `json:"min_msg_len" yaml:"min_msg_len" db:"min_msg_len"`
    MaxEmoji            int     `json:"max_emoji" yaml:"max_emoji" db:"max_emoji"`
    MinSpamProbability  float64 `json:"min_spam_probability" yaml:"min_spam_probability" db:"min_spam_probability"`
    MultiLangWords      int     `json:"multi_lang_words" yaml:"multi_lang_words" db:"multi_lang_words"`

    // Transient fields that should never be stored
    Transient TransientSettings `json:"-" yaml:"-"`
}

// TransientSettings contains settings that should never be persisted
type TransientSettings struct {
    DataBaseURL        string `json:"-" yaml:"-"`
    StorageTimeout     time.Duration `json:"-" yaml:"-"`
    ConfigDB           bool `json:"-" yaml:"-"`
    Dbg                bool `json:"-" yaml:"-"`
    TGDbg              bool `json:"-" yaml:"-"`
    ConfigDBEncryptKey string `json:"-" yaml:"-"`
    WebAuthPasswd      string `json:"-" yaml:"-"`
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
// Convert CLI options to domain settings model
appSettings := optToSettings(opts)

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

// If --confdb is set, load configuration from database
if appSettings.Transient.ConfigDB {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    
    // Save CLI credentials before loading from DB
    transient := appSettings.Transient
    telegramToken := appSettings.Telegram.Token
    openAIToken := appSettings.OpenAI.Token
    webAuthHash := appSettings.Server.AuthHash
    
    // Load from database
    if err := loadConfigFromDB(ctx, appSettings); err != nil {
        log.Printf("[ERROR] failed to load configuration from database: %v", err)
        cancel()
        os.Exit(1)
    }
    cancel()
    
    // Restore CLI values (they take precedence)
    appSettings.Transient = transient
    if telegramToken != "" {
        appSettings.Telegram.Token = telegramToken
    }
    if openAIToken != "" {
        appSettings.OpenAI.Token = openAIToken
    }
    if webAuthHash != "" {
        appSettings.Server.AuthHash = webAuthHash
    }
}
```

### 4. Web API Integration

The web API provides full configuration management capabilities:

```go
// Configuration routes are added when ConfigDB mode is enabled
if s.SettingsStore != nil && s.ConfigDBMode {
    webUI.Route(func(config *routegroup.Bundle) {
        config.HandleFunc("POST /config", s.saveConfigHandler)     // Save to DB
        config.HandleFunc("GET /config", s.loadConfigHandler)      // Load from DB
        config.HandleFunc("PUT /config", s.updateConfigHandler)    // Update settings
        config.HandleFunc("DELETE /config", s.deleteConfigHandler) // Delete from DB
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

- Storing credentials within their proper domain models (Telegram.Token, OpenAI.Token, Server.AuthHash)
- Storing the temporary password used for hash generation in the `Transient` field with `json:"-"` and `yaml:"-"` tags
- Automatically creating a safe copy of settings before database storage
- Keeping CLI credentials as the authoritative source, never overriding them with database values

### 2. CLI/DB Precedence

We've established clear precedence rules for settings:

- Database connection parameters (DataBaseURL, etc.) always come from CLI
- Security credentials (tokens, passwords) always come from CLI
- Control flags (ConfigDB, Debug modes) always come from CLI
- All other settings can come from the database if `--confdb` is enabled

### 3. Backward Compatibility

To maintain backward compatibility, we:

- Keep the existing CLI parsing with the `options` struct
- Add conversion functions between `options` and our new `Settings` type
- Preserve the original behavior when `--confdb` is not specified

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
- GET `/config` - Load configuration from database
- PUT `/config` - Update specific settings
- DELETE `/config` - Remove configuration from database

## Current Implementation Status

### ✅ Completed Components

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
   - Field-level encryption for sensitive data
   - Transient settings never persisted
   - CLI credentials always take precedence
   - Bcrypt hash generation for web authentication

### 🔄 Implementation Details

- The config package was chosen over "settings" to avoid confusion with the existing webapi Settings struct
- Credentials are stored within their domain models (e.g., `Telegram.Token`, `OpenAI.Token`)
- The system maintains backward compatibility - works exactly as before when `--confdb` is not used
- All sensitive fields are automatically encrypted when an encryption key is provided
- The implementation uses JSON for storage, making it human-readable when not encrypted

### 📊 Test Coverage

All components have been tested:
- Config package tests pass for both SQLite and PostgreSQL
- Encryption/decryption functionality verified
- Web API handlers tested
- Store operations (CRUD) fully covered

The feature is complete and ready for production use.