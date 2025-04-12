# Database Configuration Support Plan - Implementation

## Overview

This document outlines our implementation for supporting configuration parameters stored in a database for the TG-Spam application. The goal is to separate configuration concerns from CLI parameter parsing, creating a clean architecture for configuration management that supports the database as a configuration source while maintaining backward compatibility.

## Key Design Changes

We've identified and addressed an architectural issue: we were using the CLI `options` struct for database storage and a separate `Settings` struct for the web UI, with manual conversions between them. This created duplication and coupled unrelated concerns.

## Requirements

1. Create a dedicated settings model separated from CLI parsing
2. Support loading from both CLI and database with clear precedence
3. Maintain backward compatibility with existing CLI configuration
4. Support updating configuration via web interface
5. Handle sensitive information securely

## Implementation Summary

We've created a new `settings` package that provides a clean domain model for application configuration. This package includes:

1. A `Settings` struct with all configuration parameters, properly organized by domain
2. Secure handling of sensitive information via a `Transient` field
3. A dedicated `Store` for persisting settings to the database
4. Support for JSON and YAML serialization with appropriate tags

## Implementation Details

### 1. Settings Package

We've created a clean settings package that completely separates configuration concerns from CLI parsing:

```go
// app/settings/settings.go
package settings

import "time"

// Settings represents application configuration independent of source (CLI, DB, etc)
type Settings struct {
    // Core settings
    InstanceID string `json:"instance_id" yaml:"instance_id" db:"instance_id"`

    // Group settings by domain
    Telegram      TelegramSettings      `json:"telegram" yaml:"telegram"`
    Admin         AdminSettings         `json:"admin" yaml:"admin"`
    // Other domain-specific settings as separate structs...

    // Transient fields that should never be stored
    Transient TransientSettings `json:"-" yaml:"-"`
}

// Domain-specific settings types...

// TransientSettings contains settings that should never be persisted
type TransientSettings struct {
    // Connection parameters, credentials, and control flags
    DataBaseURL    string        `json:"-" yaml:"-"`
    Credentials    Credentials   `json:"-" yaml:"-"`
    // ...
}
```

### 2. Settings Store

We've implemented a dedicated storage layer that works directly with the Settings type:

```go
// app/settings/store.go
package settings

// Store provides access to settings stored in database
type Store struct {
    *engine.SQL
    engine.RWLocker
}

// Load retrieves the settings from the database
func (s *Store) Load(ctx context.Context) (*Settings, error) {
    // Implementation...
}

// Save stores the settings to the database
func (s *Store) Save(ctx context.Context, settings *Settings) error {
    // Create a safe copy without sensitive information
    safeCopy := *settings 
    safeCopy.Transient = TransientSettings{} // Clear transient fields
    
    // Save to database...
}
```

### 3. CLI Integration

In `main.go`, we'll integrate with the new settings package:

```go
// In main.go:

// Parse CLI options as usual
var opts options
p := flags.NewParser(&opts, flags.PrintErrors|flags.PassDoubleDash|flags.HelpFlag)
// ...

// If --confdb is set, load from database, otherwise create from CLI options
var appSettings *settings.Settings

if opts.ConfigDB {
    // Make database connection using CLI parameters
    db, err := makeDB(ctx, opts.DataBaseURL, opts.InstanceID)
    if err != nil {
        log.Printf("[ERROR] failed to connect to database for config: %v", err)
        os.Exit(1)
    }
    
    // Load settings from database
    store, err := settings.NewStore(ctx, db)
    if err != nil {
        log.Printf("[ERROR] failed to create settings store: %v", err)
        os.Exit(1)
    }
    
    appSettings, err = store.Load(ctx)
    if err != nil {
        log.Printf("[ERROR] failed to load settings from database: %v", err)
        os.Exit(1)
    }
    
    // Overlay critical parameters from CLI
    applyCliOverrides(appSettings, opts)
} else {
    // Create settings from CLI options
    appSettings = createSettingsFromOptions(opts)
}

// Use appSettings throughout the application...
```

### 4. Web API Integration

The web API server will directly work with the Settings type:

```go
// In webapi/server.go:

type Server struct {
    // ...
    Settings *settings.Settings
    Store    *settings.Store
}

// ConfigHandler handles GET/POST/PUT requests for configuration
func (s *Server) ConfigHandler(w http.ResponseWriter, r *http.Request) {
    // Implementation using Settings directly instead of the old webapi.Settings
}
```

## Integration Challenges and Solutions

### 1. Sensitive Information Handling

Sensitive information like API tokens and passwords must not be stored in the database. We've addressed this by:

- Placing sensitive data in a `Transient` field with `json:"-"` and `yaml:"-"` tags
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

## Current Status

### Completed

1. ✅ Designed and implemented a complete `settings` package with:
   - Well-structured domain-specific settings types
   - Secure handling of credentials and sensitive information
   - Support for multiple serialization formats (JSON, YAML, DB)

2. ✅ Implemented a `Store` component that:
   - Works with both SQLite and PostgreSQL databases
   - Handles safe storage and retrieval of settings
   - Properly protects sensitive information

3. ✅ Created comprehensive tests that:
   - Verify behavior with both SQLite and PostgreSQL
   - Test all aspects of settings storage and retrieval
   - Cover complex data structures and edge cases

### Next Steps

1. Modify `main.go` to use the new settings system:
   - Add conversion functions between CLI options and Settings
   - Update the save-config command to use the new Store

2. Update the web API to work with the Settings type directly:
   - Replace webapi.Settings with our new settings.Settings type
   - Update config handlers to work with the Settings store

3. Add integration tests to verify the full workflow:
   - CLI to database storage
   - Database to application loading
   - Web UI to database updates