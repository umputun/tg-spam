# Database Configuration Support Plan

## Overview

This document outlines the implementation plan for supporting configuration parameters stored in a database for the TG-Spam application. The goal is to allow users to optionally store and load configuration from the database instead of using command-line parameters by providing a `--confdb` flag.

## Requirements

1. Add a new CLI flag `--confdb` to trigger loading configuration from the database
2. Store configuration as a single JSON blob in the database
3. Maintain backward compatibility with existing CLI configuration
4. Support updating configuration via web interface (future enhancement)

## Implementation Plan

### 1. Database Schema

Create a new database table to store configuration as a single blob:

```sql
-- For SQLite
CREATE TABLE IF NOT EXISTS config (
    id INTEGER PRIMARY KEY,
    gid TEXT NOT NULL,
    data TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(gid)
);

-- For PostgreSQL
CREATE TABLE IF NOT EXISTS config (
    id SERIAL PRIMARY KEY,
    gid TEXT NOT NULL,
    data TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(gid)
);

CREATE INDEX IF NOT EXISTS idx_config_gid ON config(gid);
```

### 2. Storage Implementation

Create a new package in `app/storage/config.go` that will handle configuration storage and retrieval:

```go
package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/umputun/tg-spam/app/storage/engine"
)

// Config provides access to configuration stored in database
type Config struct {
	db *engine.SQL
}

// ConfigRecord represents a configuration entry
type ConfigRecord struct {
	GID       string    `db:"gid"`
	Data      string    `db:"data"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// NewConfig creates new config instance
func NewConfig(ctx context.Context, db *engine.SQL) (*Config, error) {
	if db == nil {
		return nil, fmt.Errorf("no db provided")
	}
	
	res := &Config{db: db}
	
	// Initialize the database table
	err := res.createTable(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create config table: %w", err)
	}
	
	return res, nil
}

// Get retrieves the configuration for the current group
func (c *Config) Get(ctx context.Context) (string, error) {
	var record ConfigRecord
	query := c.db.Adopt("SELECT data FROM config WHERE gid = ?")
	err := c.db.GetContext(ctx, &record, query, c.db.GID())
	if err != nil {
		return "", fmt.Errorf("failed to get config: %w", err)
	}
	return record.Data, nil
}

// GetObject retrieves the configuration and deserializes it into the provided struct
func (c *Config) GetObject(ctx context.Context, obj interface{}) error {
	data, err := c.Get(ctx)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(data), obj)
}

// Set updates or creates the configuration
func (c *Config) Set(ctx context.Context, data string) error {
	query := c.db.Adopt(`
		INSERT INTO config (gid, data, updated_at) 
		VALUES (?, ?, ?) 
		ON CONFLICT (gid) DO UPDATE 
		SET data = excluded.data, updated_at = excluded.updated_at
	`)
	_, err := c.db.ExecContext(ctx, query, c.db.GID(), data, time.Now())
	if err != nil {
		return fmt.Errorf("failed to set config: %w", err)
	}
	return nil
}

// SetObject serializes an object to JSON and stores it
func (c *Config) SetObject(ctx context.Context, obj interface{}) error {
	data, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	return c.Set(ctx, string(data))
}

// Delete removes the configuration
func (c *Config) Delete(ctx context.Context) error {
	query := c.db.Adopt("DELETE FROM config WHERE gid = ?")
	_, err := c.db.ExecContext(ctx, query, c.db.GID())
	if err != nil {
		return fmt.Errorf("failed to delete config: %w", err)
	}
	return nil
}

// createTable initializes the config table if it doesn't exist
func (c *Config) createTable(ctx context.Context) error {
	var createTableSQL string
	
	if c.db.Type() == engine.Postgres {
		createTableSQL = `
			CREATE TABLE IF NOT EXISTS config (
				id SERIAL PRIMARY KEY,
				gid TEXT NOT NULL,
				data TEXT NOT NULL,
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				UNIQUE(gid)
			);
			
			DO $$
			BEGIN
				IF NOT EXISTS (
					SELECT 1 FROM pg_indexes WHERE indexname = 'idx_config_gid'
				) THEN
					CREATE INDEX idx_config_gid ON config(gid);
				END IF;
			END $$;
		`
	} else {
		// SQLite
		createTableSQL = `
			CREATE TABLE IF NOT EXISTS config (
				id INTEGER PRIMARY KEY,
				gid TEXT NOT NULL,
				data TEXT NOT NULL,
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				UNIQUE(gid)
			);
			
			CREATE INDEX IF NOT EXISTS idx_config_gid ON config(gid);
		`
	}
	
	_, err := c.db.ExecContext(ctx, createTableSQL)
	return err
}
```

### 3. Configuration Loading Mechanism

Modify `app/main.go` to support loading configuration from the database:

1. Add a new CLI flag:

```go
type options struct {
    // existing options...
    
    ConfigDB bool `long:"confdb" env:"CONFDB" description:"load configuration from database"`
    
    // rest of the options...
}
```

2. Add a function to load configuration from the database:

```go
// loadConfigFromDB loads configuration from the database if the confdb flag is set
func loadConfigFromDB(ctx context.Context, opts *options) error {
    if !opts.ConfigDB {
        return nil // Skip if not enabled
    }
    
    log.Print("[INFO] loading configuration from database")
    
    // Create DB connection
    db, err := makeDB(ctx, *opts)
    if err != nil {
        return fmt.Errorf("failed to connect to database for config: %w", err)
    }
    
    // Create config store
    configStore, err := storage.NewConfig(ctx, db)
    if err != nil {
        return fmt.Errorf("failed to create config store: %w", err)
    }
    
    // Load configuration
    err = configStore.GetObject(ctx, opts)
    if err != nil {
        return fmt.Errorf("failed to load configuration from database: %w", err)
    }
    
    log.Printf("[INFO] configuration loaded from database successfully")
    return nil
}
```

3. Update the `main()` function to load from DB after parsing CLI flags:

```go
func main() {
    // ... existing code ...
    
    if _, err := p.Parse(); err != nil {
        if !errors.Is(err.(*flags.Error).Type, flags.ErrHelp) {
            log.Printf("[ERROR] cli error: %v", err)
            os.Exit(1)
        }
        os.Exit(2)
    }
    
    // Store original CLI values that should override DB config
    originalValues := map[string]interface{}{
        // Connection parameters that must come from CLI
        "DataBaseURL":    opts.DataBaseURL,
        "InstanceID":     opts.InstanceID,
        
        // Control flags
        "ConfigDB":       opts.ConfigDB,
        "Dbg":            opts.Dbg,
        "TGDbg":          opts.TGDbg,
        
        // Security-related parameters
        "Server.AuthPasswd": opts.Server.AuthPasswd,
        "Server.AuthHash":   opts.Server.AuthHash,
        "Telegram.Token":    opts.Telegram.Token,
        "OpenAI.Token":      opts.OpenAI.Token,
        
        // Storage configuration
        "StorageTimeout":    opts.StorageTimeout,
    }
    
    // If --confdb is set, load configuration from the database
    if opts.ConfigDB {
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        
        if err := loadConfigFromDB(ctx, &opts); err != nil {
            log.Printf("[ERROR] failed to load configuration from database: %v", err)
            os.Exit(1)
        }
        
        // Restore CLI values that should override DB config
        opts.DataBaseURL = originalValues["DataBaseURL"].(string)
        opts.InstanceID = originalValues["InstanceID"].(string)
        opts.ConfigDB = originalValues["ConfigDB"].(bool)
        opts.Dbg = originalValues["Dbg"].(bool)
        opts.TGDbg = originalValues["TGDbg"].(bool)
        opts.Server.AuthPasswd = originalValues["Server.AuthPasswd"].(string)
        opts.Server.AuthHash = originalValues["Server.AuthHash"].(string)
        opts.Telegram.Token = originalValues["Telegram.Token"].(string)
        opts.OpenAI.Token = originalValues["OpenAI.Token"].(string)
        opts.StorageTimeout = originalValues["StorageTimeout"].(time.Duration)
    }
    
    // ... rest of main ...
}
```

### 4. Saving Configuration to Database

Add a command-line option to save the current configuration to the database:

```go
// saveConfigToDB saves the current configuration to the database
func saveConfigToDB(ctx context.Context, opts *options) error {
    log.Print("[INFO] saving configuration to database")
    
    // Create DB connection
    db, err := makeDB(ctx, *opts)
    if err != nil {
        return fmt.Errorf("failed to connect to database for config: %w", err)
    }
    
    // Create config store
    configStore, err := storage.NewConfig(ctx, db)
    if err != nil {
        return fmt.Errorf("failed to create config store: %w", err)
    }
    
    // Create a copy of options to sanitize sensitive data
    configToSave := *opts
    
    // Clear sensitive values that should not be stored
    configToSave.Server.AuthPasswd = ""
    configToSave.Server.AuthHash = ""
    // Don't store tokens in DB unless explicitly requested
    if !opts.Dbg { // Use debug mode to indicate storing tokens is intentional
        configToSave.Telegram.Token = ""
        configToSave.OpenAI.Token = ""
    }
    
    // Save configuration
    err = configStore.SetObject(ctx, configToSave)
    if err != nil {
        return fmt.Errorf("failed to save configuration to database: %w", err)
    }
    
    log.Printf("[INFO] configuration saved to database successfully")
    return nil
}
```

Add a subcommand to `main.go` for saving configuration:

```go
// Add to main.go:
func main() {
    // ... existing code ...
    
    p := flags.NewParser(&opts, flags.PrintErrors|flags.PassDoubleDash|flags.HelpFlag)
    p.SubcommandsOptional = true
    
    // Add save-config command
    if _, err := p.AddCommand("save-config", "Save current configuration to database", 
                             "Saves all current settings to the database for future use with --confdb",
                             &struct{}{});
    
    if _, err := p.Parse(); err != nil {
        // ... existing error handling ...
    }
    
    // Handle commands
    if p.Active != nil && p.Active.Name == "save-config" {
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        
        if err := saveConfigToDB(ctx, &opts); err != nil {
            log.Printf("[ERROR] failed to save configuration to database: %v", err)
            os.Exit(1)
        }
        os.Exit(0)
    }
    
    // ... rest of main ...
}
```

### 5. Configuration Management in Admin UI (Future Enhancement)

In the future, add a new endpoint in the admin web interface to view and update the configuration:

1. Add a new endpoint to `webapi.go`:
```go
// ConfigHandler handles configuration operations
func (s *Server) ConfigHandler(w http.ResponseWriter, r *http.Request) {
    switch r.Method {
    case "GET":
        // Return current config (excluding sensitive information)
    case "POST":
        // Update config (preserving sensitive information from original)
    default:
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
    }
}
```

2. Create UI pages for configuration management

## Implementation Challenges

1. **Circular Dependency**: We need to connect to the database to load the configuration, but the database connection parameters might be part of the configuration. Solution: Always require the database connection parameters to be specified via CLI or env vars and never override them when loading from DB.

2. **Priority of CLI Flags**: The implementation ensures that crucial CLI flags always take precedence over database values to prevent locking users out of the system:
   - Connection parameters (DataBaseURL, InstanceID)
   - Control flags (ConfigDB, Dbg, TGDbg)
   - Security parameters (auth passwords/tokens)
   - Storage configuration

3. **Sensitive Information**: Care is taken to handle sensitive information:
   - Tokens and passwords are not stored in the database by default
   - Debug mode can be used to explicitly store tokens if needed
   - Data sanitization is performed before storing configuration

## Future Enhancements

1. Implement configuration versioning and backup/restore
2. Add web UI for viewing and editing configuration
3. Add audit logging for configuration changes
4. Support for exporting configuration to file and importing from file
5. Add ability to manage multiple configuration profiles

## Conclusion

This implementation plan provides a straightforward approach to add database configuration support to the TG-Spam application. By storing the entire configuration as a single JSON blob in the database, we maintain simplicity while providing a powerful way to manage application settings. The implementation is designed to be backward compatible with existing CLI configuration methods while providing a foundation for future enhancements.