package engine

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// Converter provides methods for database format conversion specifically for tg-spam tables.
// The primary purpose is to facilitate migration from SQLite to PostgreSQL with
// proper handling of each table's specific requirements and data types.
type Converter struct {
	db *SQL
}

// NewConverter creates a new converter for the given SQL engine
func NewConverter(db *SQL) *Converter {
	return &Converter{db: db}
}

// SqliteToPostgres converts a SQLite database to PostgreSQL format and writes it to the provided writer.
// It only converts the tables used by tg-spam: detected_spam, approved_users, samples, dictionary.
//
// The conversion process includes:
// 1. Converting table schemas with appropriate data type mappings
// 2. Converting boolean values from SQLite (0/1) to PostgreSQL (false/true)
// 3. Handling special cases like the message_hash column in samples table
// 4. Converting indices and constraints to PostgreSQL format
// 5. Exporting data using PostgreSQL's efficient COPY format
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

// convertTable exports a SQLite table in PostgreSQL format.
// This function handles the full conversion process for a single table:
// 1. Get and convert the table schema
// 2. Export the table data in PostgreSQL format
// 3. Convert and write the table's indices
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

// convertTableSchema converts a SQLite CREATE TABLE statement to PostgreSQL syntax.
// It handles specific type mappings for each of the tg-spam tables.
// This function performs both general conversions applicable to all tables
// and table-specific conversions based on the schema requirements.
func (c *Converter) convertTableSchema(tableName, sqliteStmt string) string {
	pgStmt := sqliteStmt

	// common conversions applicable to all tables:
	// 1. SQLite autoincrement primary key to PostgreSQL serial
	pgStmt = strings.ReplaceAll(pgStmt, "INTEGER PRIMARY KEY AUTOINCREMENT", "SERIAL PRIMARY KEY")
	// 2. SQLite datetime to PostgreSQL timestamp
	pgStmt = strings.ReplaceAll(pgStmt, "DATETIME", "TIMESTAMP")
	// 3. SQLite blob to PostgreSQL bytea
	pgStmt = strings.ReplaceAll(pgStmt, "BLOB", "BYTEA")

	// table-specific conversions handle unique requirements for each table
	switch tableName {
	case "detected_spam":
		// 1. user_id in detected_spam should be BIGINT to match PostgreSQL schema
		//    this is because Telegram user IDs can be very large numbers
		pgStmt = strings.ReplaceAll(pgStmt, "user_id INTEGER", "user_id BIGINT")

		// 2. Convert SQLite boolean (0) to PostgreSQL boolean (false)
		//    postgreSQL uses true/false values rather than 0/1
		pgStmt = strings.ReplaceAll(pgStmt, "added BOOLEAN DEFAULT 0", "added BOOLEAN DEFAULT false")

	case "approved_users":
		// approved_users table has a UNIQUE constraint on (gid, uid)
		// no specific conversions needed here because:
		// - The UNIQUE constraint syntax is identical in both SQLite and PostgreSQL
		// - All other type conversions are handled by the common conversions above

	case "samples":
		// the samples table requires special handling for message hashing in PostgreSQL.
		// in SQLite, we use the message text directly in the unique constraint.
		// in PostgreSQL, we create a stored computed hash column for better performance:
		if !strings.Contains(pgStmt, "message_hash") {
			// 1. Add a generated column that computes a SHA256 hash of the message text
			// 2. Change the unique constraint to use the hash instead of the full message text
			pgStmt = strings.Replace(pgStmt, "UNIQUE(gid, message)",
				"message_hash TEXT GENERATED ALWAYS AS (encode(sha256(message::bytea), 'hex')) STORED,\n"+
					"            UNIQUE(gid, message_hash)", 1)
		}

	case "dictionary":
		// dictionary table has standard types and constraints
		// no specific conversions are needed beyond the common ones applied to all tables
	}

	// final conversion of boolean defaults for any boolean columns in all tables
	pgStmt = strings.ReplaceAll(pgStmt, "BOOLEAN DEFAULT 0", "BOOLEAN DEFAULT false")
	pgStmt = strings.ReplaceAll(pgStmt, "BOOLEAN DEFAULT 1", "BOOLEAN DEFAULT true")

	return pgStmt
}

// getTableColumns returns the column names for a SQLite table.
// This is needed to properly map data during the export process.
func (c *Converter) getTableColumns(ctx context.Context, tx *sqlx.Tx, table string) ([]string, error) {
	var columns []string
	// use SQLite PRAGMA_TABLE_INFO to get column information
	query := "SELECT name FROM PRAGMA_TABLE_INFO(?)"
	if err := tx.SelectContext(ctx, &columns, query, table); err != nil {
		return nil, fmt.Errorf("failed to get columns for table %s: %w", table, err)
	}
	return columns, nil
}

// exportTableData exports data from a SQLite table in PostgreSQL COPY format.
// The COPY format is much more efficient than individual INSERT statements
// for bulk loading data into PostgreSQL.
func (c *Converter) exportTableData(ctx context.Context, tx *sqlx.Tx, w io.Writer, table string, columns []string) error {
	// get row count first to check if there's any data to export
	var count int
	if err := tx.GetContext(ctx, &count, fmt.Sprintf("SELECT COUNT(*) FROM %s", table)); err != nil {
		return fmt.Errorf("failed to get row count: %w", err)
	}

	if count == 0 {
		// no data to export, return early
		return nil
	}

	// special handling for samples table - need to exclude message_hash column for COPY
	// because it's a generated column in PostgreSQL and shouldn't be included in the COPY command
	copyColumns := columns
	if table == "samples" && len(columns) > 0 {
		// remove message_hash column for PostgreSQL COPY as it's a generated column
		filteredColumns := make([]string, 0, len(columns))
		for _, col := range columns {
			if col != "message_hash" {
				filteredColumns = append(filteredColumns, col)
			}
		}
		copyColumns = filteredColumns
	}

	//nolint:gosec // table name from internal schema, not user input
	if _, err := fmt.Fprintf(w, "-- Data for table %s\n", table); err != nil {
		return fmt.Errorf("failed to write comment: %w", err)
	}

	//nolint:gosec // table/columns from internal schema
	if _, err := fmt.Fprintf(w, "COPY %s (%s) FROM stdin;\n", table, strings.Join(copyColumns, ", ")); err != nil {
		return fmt.Errorf("failed to write COPY header: %w", err)
	}

	// query all data from the table
	query := fmt.Sprintf("SELECT * FROM %s", table)
	rows, err := tx.QueryxContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query data: %w", err)
	}
	defer rows.Close()

	// write each row in PostgreSQL COPY format (tab-separated values)
	for rows.Next() {
		row := make(map[string]any)
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
				// convert SQLite 0/1 integer boolean to PostgreSQL f/t text boolean
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

		//nolint:gosec // values from internal database rows
		if _, err := fmt.Fprintf(w, "%s\n", strings.Join(values, "\t")); err != nil {
			return fmt.Errorf("failed to write data row: %w", err)
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating rows: %w", err)
	}

	// end COPY statement with the special \. marker
	if _, err := io.WriteString(w, "\\.\n\n"); err != nil {
		return fmt.Errorf("failed to write COPY end: %w", err)
	}

	return nil
}

// convertIndices converts SQLite indices to PostgreSQL format.
// This extracts index definitions from SQLite and converts them to PostgreSQL syntax.
func (c *Converter) convertIndices(ctx context.Context, tx *sqlx.Tx, w io.Writer, table string) error {
	// get all indices for the table that have SQL definitions
	var indices []struct {
		SQL string `db:"sql"`
	}
	query := fmt.Sprintf("SELECT sql FROM sqlite_master WHERE type='index' AND tbl_name='%s' AND sql IS NOT NULL", table)
	if err := tx.SelectContext(ctx, &indices, query); err != nil {
		return fmt.Errorf("failed to get indices: %w", err)
	}

	// convert and write each index
	for _, idx := range indices {
		// convert SQLite index to PostgreSQL syntax
		pgIndex := c.convertIndexDefinition(table, idx.SQL)

		// write the converted index definition
		if _, err := fmt.Fprintf(w, "%s;\n", pgIndex); err != nil {
			return fmt.Errorf("failed to write index: %w", err)
		}
	}

	return nil
}

// convertIndexDefinition converts a SQLite CREATE INDEX statement to PostgreSQL syntax.
// This includes handling special cases for the samples table where we use message_hash instead of message.
func (c *Converter) convertIndexDefinition(tableName, sqliteStmt string) string {
	// start with basic conversion - most syntax is compatible
	pgStmt := sqliteStmt

	// postgreSQL < 9.5 doesn't support IF NOT EXISTS in index creation
	// remove it to ensure compatibility with all PostgreSQL versions
	pgStmt = strings.ReplaceAll(pgStmt, "IF NOT EXISTS", "")

	// special handling for samples table indices that reference the message column
	if tableName == "samples" {
		// we need to update any index that references the message column to use message_hash instead
		if strings.Contains(pgStmt, "ON samples(") {
			// extract the index column list between parentheses
			start := strings.Index(pgStmt, "ON samples(") + len("ON samples(")
			end := strings.Index(pgStmt[start:], ")") + start
			indexCols := pgStmt[start:end]

			// process each column in the index, replacing "message" with "message_hash"
			parts := strings.Split(indexCols, ",")
			for i, part := range parts {
				trimmed := strings.TrimSpace(part)
				if trimmed == "message" {
					parts[i] = strings.Replace(part, "message", "message_hash", 1)
				}
			}

			// join the modified columns back together and reconstruct the statement
			newIndexCols := strings.Join(parts, ",")
			pgStmt = pgStmt[:start] + newIndexCols + pgStmt[end:]
		}
	}

	return pgStmt
}

// formatPostgresValue formats a value for PostgreSQL COPY format.
// PostgreSQL COPY format requires special handling for NULL values and character escaping.
func (c *Converter) formatPostgresValue(value any) string {
	switch v := value.(type) {
	case nil:
		return "\\N" // postgreSQL COPY format for NULL

	case []byte:
		// handle binary data by converting to string and escaping special characters
		s := string(v)
		// postgreSQL COPY format requires escaping backslashes and tab/newline/return chars
		s = strings.ReplaceAll(s, "\\", "\\\\") // escape backslashes
		s = strings.ReplaceAll(s, "\t", "\\t")  // escape tabs (column separator in COPY)
		s = strings.ReplaceAll(s, "\n", "\\n")  // escape newlines (row separator in COPY)
		s = strings.ReplaceAll(s, "\r", "\\r")  // escape carriage returns
		return s

	case string:
		// handle string data by escaping special characters
		s := v
		s = strings.ReplaceAll(s, "\\", "\\\\") // escape backslashes
		s = strings.ReplaceAll(s, "\t", "\\t")  // escape tabs (column separator in COPY)
		s = strings.ReplaceAll(s, "\n", "\\n")  // escape newlines (row separator in COPY)
		s = strings.ReplaceAll(s, "\r", "\\r")  // escape carriage returns
		return s

	case time.Time:
		// format timestamps in PostgreSQL standard format
		return v.Format("2006-01-02 15:04:05")

	case bool:
		// format booleans as 't' or 'f' as required by PostgreSQL
		if v {
			return "t"
		}
		return "f"

	default:
		// for all other types, use standard string conversion
		return fmt.Sprintf("%v", v)
	}
}
