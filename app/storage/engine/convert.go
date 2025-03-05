package engine

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// Converter provides methods for database format conversion specifically for tg-spam tables
type Converter struct {
	db *SQL
}

// NewConverter creates a new converter for the given SQL engine
func NewConverter(db *SQL) *Converter {
	return &Converter{db: db}
}

// SqliteToPostgres converts a SQLite database to PostgreSQL format and writes it to the provided writer
// It only converts the tables used by tg-spam: detected_spam, approved_users, samples, dictionary
func (c *Converter) SqliteToPostgres(ctx context.Context, w io.Writer) error {
	// check if the database is SQLite
	if c.db.dbType != Sqlite {
		return fmt.Errorf("source database must be SQLite, got %s", c.db.dbType)
	}

	// begin transaction to ensure consistent backup
	tx, err := c.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// write header
	timestamp := time.Now().Format(time.RFC3339)
	header := fmt.Sprintf("-- SQLite to PostgreSQL export for tg-spam\n-- Generated: %s\n-- GID: %s\n\n", timestamp, c.db.gid)
	if _, writeErr := io.WriteString(w, header); writeErr != nil {
		return fmt.Errorf("failed to write header: %w", writeErr)
	}

	// write start transaction
	if _, writeErr := io.WriteString(w, "BEGIN;\n\n"); writeErr != nil {
		return fmt.Errorf("failed to write transaction start: %w", writeErr)
	}

	// define the tables specific to tg-spam that we want to convert
	tables := []string{"detected_spam", "approved_users", "samples", "dictionary"}

	// for each table, export schema and data if it exists
	for _, table := range tables {
		// check if table exists
		var count int
		query := "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?"
		if err := tx.GetContext(ctx, &count, query, table); err != nil {
			return fmt.Errorf("failed to check if table %s exists: %w", table, err)
		}

		if count == 0 {
			// table doesn't exist, skip it
			continue
		}

		if err := c.convertTable(ctx, tx, w, table); err != nil {
			return err
		}
	}

	// write commit transaction
	if _, err := io.WriteString(w, "COMMIT;\n"); err != nil {
		return fmt.Errorf("failed to write transaction commit: %w", err)
	}

	return nil
}

// convertTable exports a SQLite table in PostgreSQL format
func (c *Converter) convertTable(ctx context.Context, tx *sqlx.Tx, w io.Writer, table string) error {
	// get table schema information
	var createStmt string
	query := fmt.Sprintf("SELECT sql FROM sqlite_master WHERE type='table' AND name='%s'", table)
	if err := tx.GetContext(ctx, &createStmt, query); err != nil {
		return fmt.Errorf("failed to get schema for table %s: %w", table, err)
	}

	// convert SQLite schema to PostgreSQL schema
	pgCreateStmt := c.convertTableSchema(table, createStmt)

	// write converted schema
	if _, writeErr := fmt.Fprintf(w, "%s;\n\n", pgCreateStmt); writeErr != nil {
		return fmt.Errorf("failed to write schema: %w", writeErr)
	}

	// get columns for the table
	columns, err := c.getTableColumns(ctx, tx, table)
	if err != nil {
		return err
	}

	// get data and write as PostgreSQL COPY statements
	if err := c.exportTableData(ctx, tx, w, table, columns); err != nil {
		return err
	}

	// convert and write indices
	if err := c.convertIndices(ctx, tx, w, table); err != nil {
		return err
	}

	// write newline after table
	if _, err := io.WriteString(w, "\n"); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	return nil
}

// convertTableSchema converts a SQLite CREATE TABLE statement to PostgreSQL syntax
// It handles specific type mappings for each of the tg-spam tables
func (c *Converter) convertTableSchema(tableName, sqliteStmt string) string {
	pgStmt := sqliteStmt

	// common conversions for all tables
	pgStmt = strings.ReplaceAll(pgStmt, "INTEGER PRIMARY KEY AUTOINCREMENT", "SERIAL PRIMARY KEY")
	pgStmt = strings.ReplaceAll(pgStmt, "DATETIME", "TIMESTAMP")
	pgStmt = strings.ReplaceAll(pgStmt, "BLOB", "BYTEA")

	// table-specific conversions
	switch tableName {
	case "detected_spam":
		// user_id in detected_spam should be BIGINT to match PostgreSQL schema
		pgStmt = strings.ReplaceAll(pgStmt, "user_id INTEGER", "user_id BIGINT")
		// boolean handling
		pgStmt = strings.ReplaceAll(pgStmt, "added BOOLEAN DEFAULT 0", "added BOOLEAN DEFAULT false")

	case "approved_users":
		// approved_users has a UNIQUE constraint that needs to be preserved

	case "samples":
		// handle the special message_hash field for PostgreSQL
		// in SQLite we don't have it, but in PostgreSQL we need to add it
		if !strings.Contains(pgStmt, "message_hash") {
			pgStmt = strings.Replace(pgStmt, "UNIQUE(gid, message)",
				"message_hash TEXT GENERATED ALWAYS AS (encode(sha256(message::bytea), 'hex')) STORED,\n            UNIQUE(gid, message_hash)", 1)
		}

	case "dictionary":
		// dictionary table specific conversions, if any
	}

	// convert Boolean defaults for all tables
	pgStmt = strings.ReplaceAll(pgStmt, "BOOLEAN DEFAULT 0", "BOOLEAN DEFAULT false")
	pgStmt = strings.ReplaceAll(pgStmt, "BOOLEAN DEFAULT 1", "BOOLEAN DEFAULT true")

	return pgStmt
}

// getTableColumns returns the column names for a SQLite table
func (c *Converter) getTableColumns(ctx context.Context, tx *sqlx.Tx, table string) ([]string, error) {
	var columns []string
	query := "SELECT name FROM PRAGMA_TABLE_INFO(?)"
	if err := tx.SelectContext(ctx, &columns, query, table); err != nil {
		return nil, fmt.Errorf("failed to get columns for table %s: %w", table, err)
	}
	return columns, nil
}

// exportTableData exports data from a SQLite table in PostgreSQL COPY format
func (c *Converter) exportTableData(ctx context.Context, tx *sqlx.Tx, w io.Writer, table string, columns []string) error {
	// get row count first
	var count int
	if err := tx.GetContext(ctx, &count, fmt.Sprintf("SELECT COUNT(*) FROM %s", table)); err != nil {
		return fmt.Errorf("failed to get row count: %w", err)
	}

	if count == 0 {
		// no data to export
		return nil
	}

	// special handling for samples table - need to exclude message_hash column for COPY
	copyColumns := columns
	if table == "samples" && len(columns) > 0 {
		// remove message_hash column for PostgreSQL COPY as it's generated
		filteredColumns := make([]string, 0, len(columns))
		for _, col := range columns {
			if col != "message_hash" {
				filteredColumns = append(filteredColumns, col)
			}
		}
		copyColumns = filteredColumns
	}

	// write COPY statement header
	if _, err := fmt.Fprintf(w, "-- Data for table %s\n", table); err != nil {
		return fmt.Errorf("failed to write comment: %w", err)
	}

	if _, err := fmt.Fprintf(w, "COPY %s (%s) FROM stdin;\n", table, strings.Join(copyColumns, ", ")); err != nil {
		return fmt.Errorf("failed to write COPY header: %w", err)
	}

	// query data
	query := fmt.Sprintf("SELECT * FROM %s", table)
	rows, err := tx.QueryxContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query data: %w", err)
	}
	defer rows.Close()

	// write each row in PostgreSQL COPY format
	for rows.Next() {
		row := make(map[string]interface{})
		if err := rows.MapScan(row); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}

		// format values for PostgreSQL
		values := make([]string, 0, len(copyColumns))
		for _, col := range copyColumns {
			val, exists := row[col]
			if !exists {
				values = append(values, "\\N") // NULL in COPY format
				continue
			}

			// special handling for boolean values in specific tables/columns
			if table == "detected_spam" && col == "added" {
				// convert SQLite 0/1 to PostgreSQL f/t
				if v, ok := val.(int64); ok {
					if v == 0 {
						values = append(values, "f")
					} else {
						values = append(values, "t")
					}
					continue
				}
			}

			values = append(values, c.formatPostgresValue(val))
		}

		// write row
		if _, err := fmt.Fprintf(w, "%s\n", strings.Join(values, "\t")); err != nil {
			return fmt.Errorf("failed to write data row: %w", err)
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating rows: %w", err)
	}

	// end COPY statement
	if _, err := io.WriteString(w, "\\.\n\n"); err != nil {
		return fmt.Errorf("failed to write COPY end: %w", err)
	}

	return nil
}

// convertIndices converts SQLite indices to PostgreSQL format
func (c *Converter) convertIndices(ctx context.Context, tx *sqlx.Tx, w io.Writer, table string) error {
	var indices []struct {
		SQL string `db:"sql"`
	}
	query := fmt.Sprintf("SELECT sql FROM sqlite_master WHERE type='index' AND tbl_name='%s' AND sql IS NOT NULL", table)
	if err := tx.SelectContext(ctx, &indices, query); err != nil {
		return fmt.Errorf("failed to get indices: %w", err)
	}

	for _, idx := range indices {
		pgIndex := c.convertIndexDefinition(table, idx.SQL)

		if _, err := fmt.Fprintf(w, "%s;\n", pgIndex); err != nil {
			return fmt.Errorf("failed to write index: %w", err)
		}
	}

	return nil
}

// convertIndexDefinition converts a SQLite CREATE INDEX statement to PostgreSQL syntax
func (c *Converter) convertIndexDefinition(tableName, sqliteStmt string) string {
	// basic conversion - works for simple indices
	pgStmt := sqliteStmt

	// if the index has IF NOT EXISTS, remove it (PostgreSQL < 9.5 doesn't support it)
	pgStmt = strings.ReplaceAll(pgStmt, "IF NOT EXISTS", "")

	// special handling for samples table
	if tableName == "samples" {
		// use a more precise approach for the index containing "message"
		if strings.Contains(pgStmt, "ON samples(") {
			// first, extract the index column list
			start := strings.Index(pgStmt, "ON samples(") + len("ON samples(")
			end := strings.Index(pgStmt[start:], ")") + start
			indexCols := pgStmt[start:end]

			// split the columns by comma and replace just the "message" column
			parts := strings.Split(indexCols, ",")
			for i, part := range parts {
				trimmed := strings.TrimSpace(part)
				if trimmed == "message" {
					parts[i] = strings.Replace(part, "message", "message_hash", 1)
				}
			}

			// join the columns back together
			newIndexCols := strings.Join(parts, ",")

			// reconstruct the statement
			pgStmt = pgStmt[:start] + newIndexCols + pgStmt[end:]
		}
	}

	return pgStmt
}

// formatPostgresValue formats a value for PostgreSQL COPY format
func (c *Converter) formatPostgresValue(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return "\\N"
	case []byte:
		// handle special PostgreSQL COPY format escape sequences
		s := string(v)
		s = strings.ReplaceAll(s, "\\", "\\\\")
		s = strings.ReplaceAll(s, "\t", "\\t")
		s = strings.ReplaceAll(s, "\n", "\\n")
		s = strings.ReplaceAll(s, "\r", "\\r")
		return s
	case string:
		s := v
		s = strings.ReplaceAll(s, "\\", "\\\\")
		s = strings.ReplaceAll(s, "\t", "\\t")
		s = strings.ReplaceAll(s, "\n", "\\n")
		s = strings.ReplaceAll(s, "\r", "\\r")
		return s
	case time.Time:
		return v.Format("2006-01-02 15:04:05")
	case bool:
		if v {
			return "t"
		}
		return "f"
	default:
		return fmt.Sprintf("%v", v)
	}
}
