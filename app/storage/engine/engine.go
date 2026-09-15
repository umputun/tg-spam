// Package engine provides database engine abstraction for different SQL databases.
package engine

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"  // postgres driver loaded here
	_ "modernc.org/sqlite" // sqlite driver loaded here
)

// Type is a type of database engine
type Type string

// enum of supported database engines
const (
	Unknown  Type = ""
	Sqlite   Type = "sqlite"
	Postgres Type = "postgres"
	Mysql    Type = "mysql"
)

// SQL is a wrapper for sqlx.DB with type.
// Type allows distinguishing between different database engines.
type SQL struct {
	sqlx.DB
	gid    string // group id, to allow per-group storage in the same database
	dbType Type   // type of the database engine
}

// New creates a new database engine with a connection URL and group id.
// It detects the database engine type based on the connection URL and initializes
// the appropriate driver. Supports sqlite and postgres database types.
func New(ctx context.Context, connURL, gid string) (*SQL, error) {
	log.Printf("[INFO] new database engine, conn: %s, gid: %s", connURL, gid)
	if connURL == "" {
		return &SQL{}, fmt.Errorf("connection URL is empty")
	}

	switch {
	case connURL == ":memory:":
		return NewSqlite(connURL, gid)
	case strings.HasPrefix(connURL, "file://"):
		return NewSqlite(strings.TrimPrefix(connURL, "file://"), gid)
	case strings.HasPrefix(connURL, "file:"):
		return NewSqlite(strings.TrimPrefix(connURL, "file:"), gid)
	case strings.HasPrefix(connURL, "sqlite://"):
		return NewSqlite(strings.TrimPrefix(connURL, "sqlite://"), gid)
	case strings.HasSuffix(connURL, ".sqlite") || strings.HasSuffix(connURL, ".db"):
		return NewSqlite(connURL, gid)
	case strings.HasPrefix(connURL, "postgres://"):
		return NewPostgres(ctx, connURL, gid)
	}

	return &SQL{}, fmt.Errorf("unsupported database type in connection string %q", connURL)
}

// NewSqlite creates a new sqlite database
func NewSqlite(file, gid string) (*SQL, error) {
	db, err := sqlx.Connect("sqlite", file)
	if err != nil {
		return &SQL{}, fmt.Errorf("failed to connect to sqlite: %w", err)
	}
	if err := setSqlitePragma(db); err != nil {
		return &SQL{}, err
	}
	return &SQL{DB: *db, gid: gid, dbType: Sqlite}, nil
}

// NewPostgres creates a new postgres database. If the database doesn't exist, it will be created
func NewPostgres(ctx context.Context, connURL, gid string) (*SQL, error) {
	u, err := url.Parse(connURL)
	if err != nil {
		return nil, fmt.Errorf("invalid postgres connection url: %w", err)
	}

	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		return nil, fmt.Errorf("database name not specified in connection URL")
	}

	// try to connect first - database might exist
	db, err := sqlx.ConnectContext(ctx, "postgres", connURL)
	if err == nil {
		return &SQL{DB: *db, gid: gid, dbType: Postgres}, nil
	}

	// create URL for postgres database
	u.Path = "/postgres"
	baseURL := u.String()

	// connect to default postgres database
	baseDB, err := sqlx.ConnectContext(ctx, "postgres", baseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}
	defer baseDB.Close()

	// create database
	_, err = baseDB.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE %q`, dbName))
	if err != nil {
		// ignore error if database already exists
		if !strings.Contains(err.Error(), "already exists") {
			return nil, fmt.Errorf("failed to create database: %w", err)
		}
	}
	log.Printf("[INFO] created database %s", dbName) //nolint:gosec // dbName from internal config, not user input

	// connect to the new database
	db, err = sqlx.ConnectContext(ctx, "postgres", connURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to created database: %w", err)
	}
	return &SQL{DB: *db, gid: gid, dbType: Postgres}, nil
}

// GID returns the group id
func (e *SQL) GID() string {
	return e.gid
}

// Type returns the database engine type
func (e *SQL) Type() Type {
	return e.dbType
}

// SetDBType sets the database engine type (used for testing)
func (e *SQL) SetDBType(dbType Type) {
	e.dbType = dbType
}

// MakeLock creates a new lock for the database engine
func (e *SQL) MakeLock() RWLocker {
	if e.dbType == Sqlite {
		return new(sync.RWMutex) // sqlite need locking
	}
	return &NoopLocker{} // other engines don't need locking
}

// Backup creates a database backup and writes it to the provided writer.
// For SQLite, it backs up all tables.
// For PostgreSQL, it backs up data with the current GID where applicable.
func (e *SQL) Backup(ctx context.Context, w io.Writer) error {
	switch e.dbType {
	case Sqlite:
		return e.backupSqlite(ctx, w)
	case Postgres:
		return e.backupPostgres(ctx, w)
	default:
		return fmt.Errorf("backup not supported for database type: %s", e.dbType)
	}
}

// BackupSqliteAsPostgres creates a PostgreSQL-compatible backup from a SQLite database
// and writes it to the provided writer.
func (e *SQL) BackupSqliteAsPostgres(ctx context.Context, w io.Writer) error {
	converter := NewConverter(e)
	return converter.SqliteToPostgres(ctx, w)
}

// Adopt adopts placeholders in a query for the database engine
// This is for query which compatible between sqlite and postgres except for the placeholders
func (e *SQL) Adopt(q string) string {
	if e.dbType != Postgres { // no need to replace placeholders for sqlite
		return q
	}

	placeholderCount := 1
	var result strings.Builder
	result.Grow(len(q))
	inQuotes := false

	for _, r := range q {
		switch r {
		case '\'':
			inQuotes = !inQuotes
			result.WriteRune(r)
		case '?':
			if inQuotes {
				result.WriteRune(r)
			} else {
				result.WriteString("$" + strconv.Itoa(placeholderCount))
				placeholderCount++
			}
		default:
			result.WriteRune(r)
		}
	}

	return result.String()
}

func setSqlitePragma(db *sqlx.DB) error {
	// set pragmas for SQLite. Commented out pragmas as they are not used in the code yet because we need
	// to make sure if it is worth having 2 more DB-related files for WAL and SHM.
	pragmas := map[string]string{
		"journal_mode": "DELETE", // explicitly set to DELETE mode to prevent WAL files
		// "journal_mode": "WAL",
		// "synchronous":  "NORMAL",
		// "busy_timeout": "5000",
		// "foreign_keys": "ON",
	}

	// set pragma
	for name, value := range pragmas {
		if _, err := db.Exec("PRAGMA " + name + " = " + value); err != nil {
			return fmt.Errorf("failed to set pragma %s=%s: %w", name, value, err)
		}
	}
	return nil
}

// backupSqlite performs a backup of the SQLite database and writes the SQL statements
// to create and populate the database to the provided writer.
func (e *SQL) backupSqlite(ctx context.Context, w io.Writer) error {
	// begin transaction to ensure consistent backup
	tx, err := e.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// write header
	if headerErr := e.writeSqliteHeader(w); headerErr != nil {
		return headerErr
	}

	// get list of all tables
	tables, err := e.getSqliteTables(ctx, tx)
	if err != nil {
		return err
	}

	// for each table, export schema and data
	for _, table := range tables {
		if tableErr := e.backupSqliteTable(ctx, tx, w, table); tableErr != nil {
			return tableErr
		}
	}

	return nil
}

// writeSqliteHeader writes the SQLite backup header to the writer
func (e *SQL) writeSqliteHeader(w io.Writer) error {
	timestamp := time.Now().Format(time.RFC3339)
	header := fmt.Sprintf("-- SQLite database backup\n-- Generated: %s\n-- GID: %s\n\n", timestamp, e.gid)
	if _, err := io.WriteString(w, header); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}
	return nil
}

// getSqliteTables returns a list of all user tables in the SQLite database
func (e *SQL) getSqliteTables(ctx context.Context, tx *sqlx.Tx) ([]string, error) {
	var tables []string
	query := "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'"
	if err := tx.SelectContext(ctx, &tables, query); err != nil {
		return nil, fmt.Errorf("failed to get tables: %w", err)
	}
	return tables, nil
}

// backupSqliteTable handles the backup of a single SQLite table
func (e *SQL) backupSqliteTable(ctx context.Context, tx *sqlx.Tx, w io.Writer, table string) error {
	// write table schema
	if err := e.writeSqliteTableSchema(ctx, tx, w, table); err != nil {
		return err
	}

	// write indices
	if err := e.writeSqliteTableIndices(ctx, tx, w, table); err != nil {
		return err
	}

	// write newline after indices
	if _, err := io.WriteString(w, "\n"); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	// write table data
	columns, err := e.getSqliteColumns(ctx, tx, table)
	if err != nil {
		return err
	}

	if err := e.writeSqliteTableData(ctx, tx, w, table, columns); err != nil {
		return err
	}

	// add newline after table data
	if _, err := io.WriteString(w, "\n"); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	return nil
}

// writeSqliteTableSchema writes the CREATE TABLE statement for a SQLite table
func (e *SQL) writeSqliteTableSchema(ctx context.Context, tx *sqlx.Tx, w io.Writer, table string) error {
	var createStmt string
	query := fmt.Sprintf("SELECT sql FROM sqlite_master WHERE type='table' AND name='%s'", table)
	if err := tx.GetContext(ctx, &createStmt, query); err != nil {
		return fmt.Errorf("failed to get schema for table %s: %w", table, err)
	}

	if _, err := fmt.Fprintf(w, "%s;\n\n", createStmt); err != nil {
		return fmt.Errorf("failed to write schema for table %s: %w", table, err)
	}
	return nil
}

// writeSqliteTableIndices writes the CREATE INDEX statements for a SQLite table
func (e *SQL) writeSqliteTableIndices(ctx context.Context, tx *sqlx.Tx, w io.Writer, table string) error {
	var indices []struct {
		SQL string `db:"sql"`
	}
	query := fmt.Sprintf("SELECT sql FROM sqlite_master WHERE type='index' AND tbl_name='%s' AND sql IS NOT NULL", table)
	if err := tx.SelectContext(ctx, &indices, query); err != nil {
		return fmt.Errorf("failed to get indices for table %s: %w", table, err)
	}

	for _, idx := range indices {
		if _, err := fmt.Fprintf(w, "%s;\n", idx.SQL); err != nil {
			return fmt.Errorf("failed to write index: %w", err)
		}
	}
	return nil
}

// getSqliteColumns returns the column names for a SQLite table
func (e *SQL) getSqliteColumns(ctx context.Context, tx *sqlx.Tx, table string) ([]string, error) {
	var columns []string
	query := "SELECT name FROM PRAGMA_TABLE_INFO(?)"
	if err := tx.SelectContext(ctx, &columns, query, table); err != nil {
		return nil, fmt.Errorf("failed to get columns for table %s: %w", table, err)
	}
	return columns, nil
}

// writeSqliteTableData writes the data for a SQLite table as INSERT statements
func (e *SQL) writeSqliteTableData(ctx context.Context, tx *sqlx.Tx, w io.Writer, table string, columns []string) error {
	// get data from the table
	query := fmt.Sprintf("SELECT * FROM %s", table)
	rows, err := tx.QueryxContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query data from table %s: %w", table, err)
	}

	// close rows when done - not using defer in the loop
	defer func() {
		closeErr := rows.Close()
		if closeErr != nil {
			log.Printf("[WARN] failed to close rows: %v", closeErr)
		}
	}()

	// write INSERT statements for each row
	for rows.Next() {
		if writeErr := e.writeSqliteRow(w, rows, table, columns); writeErr != nil {
			return writeErr
		}
	}

	if err = rows.Err(); err != nil {
		return fmt.Errorf("error iterating rows: %w", err)
	}

	return nil
}

// writeSqliteRow writes a single row as an INSERT statement
func (e *SQL) writeSqliteRow(w io.Writer, rows *sqlx.Rows, table string, columns []string) error {
	row := make(map[string]any)
	if err := rows.MapScan(row); err != nil {
		return fmt.Errorf("failed to scan row: %w", err)
	}

	// build column names and values for INSERT statement
	// pre-allocate to avoid reallocation
	cols := make([]string, 0, len(columns))
	vals := make([]string, 0, len(columns))

	for _, col := range columns {
		// only include columns that are in the row
		value, ok := row[col]
		if !ok {
			continue
		}

		cols = append(cols, col)
		vals = append(vals, e.formatSqliteValue(value))
	}

	insertStmt := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s);",
		table, strings.Join(cols, ", "), strings.Join(vals, ", "))

	if _, err := fmt.Fprintln(w, insertStmt); err != nil {
		return fmt.Errorf("failed to write insert statement: %w", err)
	}

	return nil
}

// formatSqliteValue formats a value for SQLite INSERT statement
func (e *SQL) formatSqliteValue(value any) string {
	switch v := value.(type) {
	case nil:
		return "NULL"
	case int, int64, float64:
		return fmt.Sprintf("%v", v)
	case []byte:
		return fmt.Sprintf("'%s'", strings.ReplaceAll(string(v), "'", "''"))
	case string:
		return fmt.Sprintf("'%s'", strings.ReplaceAll(v, "'", "''"))
	case time.Time:
		return fmt.Sprintf("'%s'", v.Format("2006-01-02 15:04:05"))
	default:
		return fmt.Sprintf("'%v'", v)
	}
}

// backupPostgres performs a backup of the PostgreSQL database and writes SQL statements
// to recreate the data to the provided writer. It filters data by the current gid.
func (e *SQL) backupPostgres(ctx context.Context, w io.Writer) error {
	// begin transaction to ensure consistent backup
	tx, err := e.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// write header and transaction start
	if headerErr := e.writePostgresHeader(ctx, tx, w); headerErr != nil {
		return headerErr
	}

	// get list of all tables
	tables, err := e.getPostgresTables(ctx, tx)
	if err != nil {
		return err
	}

	// for each table, export schema and data
	for _, table := range tables {
		if tableErr := e.backupPostgresTable(ctx, tx, w, table); tableErr != nil {
			return tableErr
		}
	}

	// write commit transaction
	if _, writeErr := io.WriteString(w, "COMMIT;\n"); writeErr != nil {
		return fmt.Errorf("failed to write transaction commit: %w", writeErr)
	}

	return nil
}

// writePostgresHeader writes the PostgreSQL backup header information
func (e *SQL) writePostgresHeader(ctx context.Context, tx *sqlx.Tx, w io.Writer) error {
	// write header with backup information
	timestamp := time.Now().Format(time.RFC3339)
	header := fmt.Sprintf("-- PostgreSQL database backup\n-- Generated: %s\n-- GID: %s\n\n", timestamp, e.gid)
	if _, err := io.WriteString(w, header); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	// check if database is PostgreSQL
	var version string
	if err := tx.GetContext(ctx, &version, "SELECT version()"); err != nil {
		return fmt.Errorf("failed to get PostgreSQL version: %w", err)
	}
	if _, err := fmt.Fprintf(w, "-- PostgreSQL Version: %s\n\n", version); err != nil {
		return fmt.Errorf("failed to write version: %w", err)
	}

	// write start transaction
	if _, err := io.WriteString(w, "BEGIN;\n\n"); err != nil {
		return fmt.Errorf("failed to write transaction start: %w", err)
	}

	return nil
}

// getPostgresTables returns a list of all user tables in the PostgreSQL database
func (e *SQL) getPostgresTables(ctx context.Context, tx *sqlx.Tx) ([]string, error) {
	var tables []string
	query := `
		SELECT tablename
		FROM pg_catalog.pg_tables
		WHERE schemaname != 'pg_catalog'
		AND schemaname != 'information_schema'
	`
	if err := tx.SelectContext(ctx, &tables, query); err != nil {
		return nil, fmt.Errorf("failed to get tables: %w", err)
	}
	return tables, nil
}

// backupPostgresTable handles the backup of a single PostgreSQL table
func (e *SQL) backupPostgresTable(ctx context.Context, tx *sqlx.Tx, w io.Writer, table string) error {
	// write table schema
	if err := e.writePostgresTableSchema(ctx, tx, w, table); err != nil {
		return err
	}

	// check if table has gid column and get columns
	hasGID, columns, err := e.getPostgresTableInfo(ctx, tx, table)
	if err != nil {
		return err
	}

	// write table data
	if err := e.writePgTableData(ctx, tx, w, table, columns, hasGID); err != nil {
		// skip table data if there are no rows
		if !strings.Contains(err.Error(), "no rows in result set") {
			return err
		}
	}

	// write indices
	if err := e.writePostgresTableIndices(ctx, tx, w, table); err != nil {
		return err
	}

	return nil
}

// writePostgresTableSchema writes the CREATE TABLE statement for a PostgreSQL table
func (e *SQL) writePostgresTableSchema(ctx context.Context, tx *sqlx.Tx, w io.Writer, table string) error {
	var createStmt string
	query := fmt.Sprintf(`
		SELECT
			'CREATE TABLE ' || table_name || ' (' ||
			array_to_string(
				array_agg(
					column_name || ' ' ||
					data_type ||
					CASE
						WHEN character_maximum_length IS NOT NULL THEN '(' || character_maximum_length || ')'
						ELSE ''
					END ||
					CASE
						WHEN is_nullable = 'NO' THEN ' NOT NULL'
						ELSE ''
					END
				), ', '
			) || ');' as create_statement
		FROM information_schema.columns
		WHERE table_name = '%s'
		GROUP BY table_name
	`, table)
	if err := tx.GetContext(ctx, &createStmt, query); err != nil {
		return fmt.Errorf("failed to get schema for table %s: %w", table, err)
	}

	// write create table statement
	if _, err := fmt.Fprintf(w, "%s\n\n", createStmt); err != nil {
		return fmt.Errorf("failed to write schema for table %s: %w", table, err)
	}

	return nil
}

// getPostgresTableInfo gets column information and checks if the table has a GID column
func (e *SQL) getPostgresTableInfo(ctx context.Context, tx *sqlx.Tx, table string) (hasGID bool, coumns []string, err error) {
	// check if the table has a GID column
	query := `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_name = $1
			AND column_name = 'gid'
		)
	`
	if err := tx.GetContext(ctx, &hasGID, query, table); err != nil {
		return false, nil, fmt.Errorf("failed to check if table %s has gid column: %w", table, err)
	}

	// get table columns
	var columns []string
	query = `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_name = $1
		ORDER BY ordinal_position
	`
	if err := tx.SelectContext(ctx, &columns, query, table); err != nil {
		return false, nil, fmt.Errorf("failed to get columns for table %s: %w", table, err)
	}

	return hasGID, columns, nil
}

// writePgTableData writes the data rows for a PostgreSQL table
func (e *SQL) writePgTableData(ctx context.Context, tx *sqlx.Tx, w io.Writer, tbl string, cols []string, hasGID bool) error {
	// build query to get data
	dataQuery := fmt.Sprintf("SELECT * FROM %s", tbl)
	if hasGID {
		dataQuery = fmt.Sprintf("SELECT * FROM %s WHERE gid = $1", tbl)
	}

	// check if there are any rows first
	var count int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM (%s) as subq", dataQuery)
	var err error
	if hasGID {
		err = tx.GetContext(ctx, &count, countQuery, e.gid)
	} else {
		err = tx.GetContext(ctx, &count, countQuery)
	}
	if err != nil {
		return fmt.Errorf("failed to check row count for table %s: %w", tbl, err)
	}

	if count == 0 {
		// skip if no data
		return nil
	}

	// write data comment
	if _, commentErr := fmt.Fprintf(w, "-- Data for table %s\n", tbl); commentErr != nil {
		return fmt.Errorf("failed to write comment: %w", commentErr)
	}

	// start COPY statement
	if _, copyErr := fmt.Fprintf(w, "COPY %s (%s) FROM stdin;\n", tbl, strings.Join(cols, ", ")); copyErr != nil {
		return fmt.Errorf("failed to write COPY statement: %w", copyErr)
	}

	// execute query to get rows
	var rows *sqlx.Rows
	if hasGID {
		rows, err = tx.QueryxContext(ctx, dataQuery, e.gid)
	} else {
		rows, err = tx.QueryxContext(ctx, dataQuery)
	}
	if err != nil {
		return fmt.Errorf("failed to query data from table %s: %w", tbl, err)
	}

	// important: use a function to properly close rows at the end
	defer func() {
		closeErr := rows.Close()
		if closeErr != nil {
			log.Printf("[WARN] failed to close rows: %v", closeErr)
		}
	}()

	// process rows
	for rows.Next() {
		if writeErr := e.writePostgresRow(w, rows, cols); writeErr != nil {
			return writeErr
		}
	}

	if rowErr := rows.Err(); rowErr != nil {
		return fmt.Errorf("error iterating rows: %w", rowErr)
	}

	// end COPY statement
	if _, err := io.WriteString(w, "\\.\n\n"); err != nil {
		return fmt.Errorf("failed to write COPY end: %w", err)
	}

	return nil
}

// writePostgresRow formats and writes a single row in PostgreSQL COPY format
func (e *SQL) writePostgresRow(w io.Writer, rows *sqlx.Rows, columns []string) error {
	row := make(map[string]any)
	if err := rows.MapScan(row); err != nil {
		return fmt.Errorf("failed to scan row: %w", err)
	}

	// pre-allocate to avoid reallocation
	values := make([]string, 0, len(columns))
	for _, col := range columns {
		val, ok := row[col]
		if !ok {
			values = append(values, "\\N") // NULL in COPY format
			continue
		}
		values = append(values, e.formatPostgresValue(val))
	}

	if _, err := fmt.Fprintf(w, "%s\n", strings.Join(values, "\t")); err != nil {
		return fmt.Errorf("failed to write data row: %w", err)
	}

	return nil
}

// formatPostgresValue formats a value for PostgreSQL COPY format
func (e *SQL) formatPostgresValue(value any) string {
	switch v := value.(type) {
	case nil:
		return "\\N"
	case []byte:
		// escape special characters in COPY format
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
	default:
		return fmt.Sprintf("%v", v)
	}
}

// writePostgresTableIndices writes the index definitions for a PostgreSQL table
func (e *SQL) writePostgresTableIndices(ctx context.Context, tx *sqlx.Tx, w io.Writer, table string) error {
	var indices []struct {
		Name       string `db:"indexname"`
		Definition string `db:"indexdef"`
	}
	query := `
		SELECT indexname, indexdef
		FROM pg_indexes
		WHERE tablename = $1
		AND indexname NOT LIKE '%_pkey'
	`
	if err := tx.SelectContext(ctx, &indices, query, table); err != nil {
		return fmt.Errorf("failed to get indices for table %s: %w", table, err)
	}

	for _, idx := range indices {
		if _, err := fmt.Fprintf(w, "%s;\n", idx.Definition); err != nil {
			return fmt.Errorf("failed to write index: %w", err)
		}
	}

	if len(indices) > 0 {
		if _, err := io.WriteString(w, "\n"); err != nil {
			return fmt.Errorf("failed to write newline: %w", err)
		}
	}

	return nil
}

// TableConfig represents configuration for table initialization
type TableConfig struct {
	Name          string
	CreateTable   DBCmd
	CreateIndexes DBCmd
	MigrateFunc   func(ctx context.Context, tx *sqlx.Tx, gid string) error
	QueriesMap    *QueryMap
}

// InitTable initializes database table with schema and handles migration in a transaction
func InitTable(ctx context.Context, db *SQL, cfg TableConfig) error {
	if db == nil {
		return fmt.Errorf("db connection is nil")
	}

	tx, err := db.Beginx()
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	// create table first
	createSchema, err := cfg.QueriesMap.Pick(db.Type(), cfg.CreateTable)
	if err != nil {
		return fmt.Errorf("failed to get create table query: %w", err)
	}
	if _, err = tx.ExecContext(ctx, createSchema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// try to migrate if needed
	if err = cfg.MigrateFunc(ctx, tx, db.GID()); err != nil {
		return fmt.Errorf("failed to migrate table: %w", err)
	}

	// create indices after migration when all columns exist
	createIndexes, err := cfg.QueriesMap.Pick(db.Type(), cfg.CreateIndexes)
	if err != nil {
		return fmt.Errorf("failed to get create indexes query: %w", err)
	}
	if _, err = tx.ExecContext(ctx, createIndexes); err != nil {
		return fmt.Errorf("failed to create indexes: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}
