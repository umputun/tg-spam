package engine

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-pkgz/testutils/containers"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConverter_SqliteToPostgres(t *testing.T) {
	ctx := context.Background()

	// create a temp SQLite DB to test conversion
	tmp, err := os.CreateTemp("", "test-convert-*.db")
	require.NoError(t, err)
	defer os.Remove(tmp.Name())

	// create a test SQLite DB
	db, err := NewSqlite(tmp.Name(), "test")
	require.NoError(t, err)
	defer db.Close()

	// create detected_spam table (one of the actual tables used in tg-spam)
	_, err = db.Exec(`CREATE TABLE detected_spam (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		gid TEXT NOT NULL DEFAULT '',
		text TEXT,
		user_id INTEGER,
		user_name TEXT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		added BOOLEAN DEFAULT 0,
		checks TEXT
	)`)
	require.NoError(t, err)

	// create approved_users table
	_, err = db.Exec(`CREATE TABLE approved_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		uid TEXT,
		gid TEXT DEFAULT '',
		name TEXT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(gid, uid)
	)`)
	require.NoError(t, err)

	// create samples table
	_, err = db.Exec(`CREATE TABLE samples (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		gid TEXT NOT NULL DEFAULT '',
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		type TEXT CHECK (type IN ('ham', 'spam')),
		origin TEXT CHECK (origin IN ('preset', 'user')),
		message TEXT NOT NULL,
		UNIQUE(gid, message)
	)`)
	require.NoError(t, err)

	// create dictionary table
	_, err = db.Exec(`CREATE TABLE dictionary (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		gid TEXT DEFAULT '',
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		type TEXT CHECK (type IN ('stop_phrase', 'ignored_word')),
		data TEXT NOT NULL,
		UNIQUE(gid, data)
	)`)
	require.NoError(t, err)

	// insert test data
	_, err = db.Exec(`INSERT INTO detected_spam (gid, text, user_id, user_name, added, checks) 
		VALUES (?, ?, ?, ?, ?, ?)`,
		"test", "Test spam message", 12345, "user1", 0, `[{"name":"test","spam":true,"details":"test details"}]`)
	require.NoError(t, err)

	// add another row with added=1 to test boolean conversion
	_, err = db.Exec(`INSERT INTO detected_spam (gid, text, user_id, user_name, added, checks) 
		VALUES (?, ?, ?, ?, ?, ?)`,
		"test", "Another spam message", 67890, "user2", 1, `[{"name":"test2","spam":true,"details":"test details 2"}]`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO approved_users (uid, gid, name) 
		VALUES (?, ?, ?)`,
		"user123", "test", "Test User")
	require.NoError(t, err)

	// insert data into samples table
	_, err = db.Exec(`INSERT INTO samples (gid, type, origin, message) 
		VALUES (?, ?, ?, ?)`,
		"test", "spam", "user", "Sample spam message")
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO samples (gid, type, origin, message) 
		VALUES (?, ?, ?, ?)`,
		"test", "ham", "preset", "Sample ham message")
	require.NoError(t, err)

	// insert data into dictionary table
	_, err = db.Exec(`INSERT INTO dictionary (gid, type, data) 
		VALUES (?, ?, ?)`,
		"test", "stop_phrase", "spam phrase")
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO dictionary (gid, type, data) 
		VALUES (?, ?, ?)`,
		"test", "ignored_word", "ignore")
	require.NoError(t, err)

	// add indices
	_, err = db.Exec(`CREATE INDEX idx_detected_spam_gid ON detected_spam (gid)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE INDEX idx_approved_users_gid ON approved_users (gid)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE INDEX idx_samples_message ON samples(message)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE INDEX idx_dictionary_phrase ON dictionary(data)`)
	require.NoError(t, err)

	// create converter
	converter := NewConverter(db)

	// test conversion
	var buf bytes.Buffer
	err = converter.SqliteToPostgres(ctx, &buf)
	require.NoError(t, err)

	// check conversion result
	result := buf.String()
	t.Logf("Conversion result: %s", result)

	// verify it contains basic components
	assert.Contains(t, result, "BEGIN;")
	assert.Contains(t, result, "COMMIT;")

	// check detected_spam table conversion
	assert.Contains(t, result, "CREATE TABLE detected_spam")
	assert.Contains(t, result, "SERIAL PRIMARY KEY")          // converted from INTEGER PRIMARY KEY AUTOINCREMENT
	assert.Contains(t, result, "TIMESTAMP")                   // converted from DATETIME
	assert.Contains(t, result, "user_id BIGINT")              // converted from INTEGER
	assert.Contains(t, result, "added BOOLEAN DEFAULT false") // converted from DEFAULT 0

	// check approved_users table conversion
	assert.Contains(t, result, "CREATE TABLE approved_users")
	assert.Contains(t, result, "UNIQUE(gid, uid)") // preserved unique constraint

	// check samples table conversion
	assert.Contains(t, result, "CREATE TABLE samples")
	assert.Contains(t, result, "message_hash TEXT GENERATED ALWAYS AS") // added hash column
	assert.Contains(t, result, "UNIQUE(gid, message_hash)")             // changed unique constraint

	// check dictionary table conversion
	assert.Contains(t, result, "CREATE TABLE dictionary")
	assert.Contains(t, result, "CHECK (type IN ('stop_phrase', 'ignored_word'))")

	// check indices
	assert.Contains(t, result, "CREATE INDEX idx_detected_spam_gid")
	assert.Contains(t, result, "CREATE INDEX idx_approved_users_gid")
	assert.Contains(t, result, "CREATE INDEX idx_samples_message")
	assert.Contains(t, result, "message_hash") // should convert message index to message_hash
	assert.Contains(t, result, "CREATE INDEX idx_dictionary_phrase")

	// check COPY statements and data
	assert.Contains(t, result, "COPY detected_spam")
	assert.Contains(t, result, "Test spam message")
	assert.Contains(t, result, "Another spam message")
	assert.Contains(t, result, "COPY approved_users")
	assert.Contains(t, result, "Test User")
	assert.Contains(t, result, "COPY samples")
	assert.Contains(t, result, "Sample spam message")
	assert.Contains(t, result, "Sample ham message")
	assert.Contains(t, result, "COPY dictionary")
	assert.Contains(t, result, "spam phrase")
	assert.Contains(t, result, "ignore")

	// check special value conversion
	t.Run("boolean conversion", func(t *testing.T) {
		// look for 'f' and 't' in the output for boolean values (added column)
		lines := strings.Split(result, "\n")
		foundFalse := false
		foundTrue := false

		for _, line := range lines {
			if strings.Contains(line, "Test spam message") {
				assert.Contains(t, line, "\tf\t") // false value
				foundFalse = true
			}
			if strings.Contains(line, "Another spam message") {
				assert.Contains(t, line, "\tt\t") // true value
				foundTrue = true
			}
		}

		assert.True(t, foundFalse, "Should convert '0' to 'f' for boolean values")
		assert.True(t, foundTrue, "Should convert '1' to 't' for boolean values")
	})
}

func TestConverter_SqliteToPostgres_NonSqliteError(t *testing.T) {
	ctx := context.Background()

	// create a mock PostgreSQL db
	mockDB := &SQL{dbType: Postgres, gid: "test"}

	converter := NewConverter(mockDB)

	// test conversion from PostgreSQL should fail
	var buf bytes.Buffer
	err := converter.SqliteToPostgres(ctx, &buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "source database must be SQLite")
}

func TestConverter_ConvertTableSchema(t *testing.T) {
	converter := NewConverter(&SQL{})

	tests := []struct {
		name       string
		tableName  string
		sqliteStmt string
		expected   string
	}{
		{
			name:       "Convert INTEGER PRIMARY KEY",
			tableName:  "test",
			sqliteStmt: "CREATE TABLE test (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT)",
			expected:   "CREATE TABLE test (id SERIAL PRIMARY KEY, name TEXT)",
		},
		{
			name:       "Convert DATETIME",
			tableName:  "test",
			sqliteStmt: "CREATE TABLE test (created DATETIME DEFAULT CURRENT_TIMESTAMP)",
			expected:   "CREATE TABLE test (created TIMESTAMP DEFAULT CURRENT_TIMESTAMP)",
		},
		{
			name:       "Convert BLOB",
			tableName:  "test",
			sqliteStmt: "CREATE TABLE test (data BLOB)",
			expected:   "CREATE TABLE test (data BYTEA)",
		},
		{
			name:       "Convert detected_spam table",
			tableName:  "detected_spam",
			sqliteStmt: "CREATE TABLE detected_spam (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER, added BOOLEAN DEFAULT 0)",
			expected:   "CREATE TABLE detected_spam (id SERIAL PRIMARY KEY, user_id BIGINT, added BOOLEAN DEFAULT false)",
		},
		{
			name:       "Convert samples table",
			tableName:  "samples",
			sqliteStmt: "CREATE TABLE samples (id INTEGER PRIMARY KEY AUTOINCREMENT, message TEXT, UNIQUE(gid, message))",
			expected:   "CREATE TABLE samples (id SERIAL PRIMARY KEY, message TEXT, message_hash TEXT GENERATED ALWAYS AS (encode(sha256(message::bytea), 'hex')) STORED,\n            UNIQUE(gid, message_hash))",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := converter.convertTableSchema(tt.tableName, tt.sqliteStmt)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConverter_ConvertIndexDefinition(t *testing.T) {
	converter := NewConverter(&SQL{})

	tests := []struct {
		name       string
		tableName  string
		sqliteStmt string
		expected   string
	}{
		{
			name:       "Simple index",
			tableName:  "test",
			sqliteStmt: "CREATE INDEX IF NOT EXISTS idx_test ON test (name)",
			expected:   "CREATE INDEX  idx_test ON test (name)",
		},
		{
			name:       "Sample message index",
			tableName:  "samples",
			sqliteStmt: "CREATE INDEX idx_samples_message ON samples(message)",
			expected:   "CREATE INDEX idx_samples_message ON samples(message_hash)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := converter.convertIndexDefinition(tt.tableName, tt.sqliteStmt)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConverter_FormatPostgresValue(t *testing.T) {
	converter := NewConverter(&SQL{})

	tests := []struct {
		name     string
		value    interface{}
		expected string
	}{
		{
			name:     "Format nil",
			value:    nil,
			expected: "\\N",
		},
		{
			name:     "Format string",
			value:    "test",
			expected: "test",
		},
		{
			name:     "Format string with escapes",
			value:    "test\nline\twith\rspecial\\chars",
			expected: "test\\nline\\twith\\rspecial\\\\chars",
		},
		{
			name:     "Format bytes",
			value:    []byte("test\ndata"),
			expected: "test\\ndata",
		},
		{
			name:     "Format bool true",
			value:    true,
			expected: "t",
		},
		{
			name:     "Format bool false",
			value:    false,
			expected: "f",
		},
		{
			name:     "Format number",
			value:    42,
			expected: "42",
		},
		{
			name:     "Format time.Time",
			value:    time.Date(2023, 5, 15, 10, 30, 0, 0, time.UTC),
			expected: "2023-05-15 10:30:00",
		},
		{
			name:     "Format tab and newline characters in string",
			value:    "multi\nline\ttext",
			expected: "multi\\nline\\ttext",
		},
		{
			name:     "Format empty string",
			value:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := converter.formatPostgresValue(tt.value)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConverter_ExportTableData_Empty(t *testing.T) {
	ctx := context.Background()

	// create a temp SQLite DB
	tmp, err := os.CreateTemp("", "test-empty-table-*.db")
	require.NoError(t, err)
	defer os.Remove(tmp.Name())

	// create a test SQLite DB
	db, err := NewSqlite(tmp.Name(), "test")
	require.NoError(t, err)
	defer db.Close()

	// create an empty table
	_, err = db.Exec(`CREATE TABLE empty_table (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT
	)`)
	require.NoError(t, err)

	// begin transaction for testing
	tx, err := db.BeginTxx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()

	// get columns for the table
	columns, err := (&Converter{db: db}).getTableColumns(ctx, tx, "empty_table")
	require.NoError(t, err)
	assert.Equal(t, []string{"id", "name"}, columns)

	// test exporting empty table
	var buf bytes.Buffer
	converter := NewConverter(db)
	err = converter.exportTableData(ctx, tx, &buf, "empty_table", columns)
	require.NoError(t, err)

	// an empty table should not produce any COPY statements
	assert.Equal(t, "", buf.String())
}

func TestConverter_ExportTableData_WithNullValues(t *testing.T) {
	ctx := context.Background()

	// create a temp SQLite DB
	tmp, err := os.CreateTemp("", "test-null-values-*.db")
	require.NoError(t, err)
	defer os.Remove(tmp.Name())

	// create a test SQLite DB
	db, err := NewSqlite(tmp.Name(), "test")
	require.NoError(t, err)
	defer db.Close()

	// create a table
	_, err = db.Exec(`CREATE TABLE null_test (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		text_value TEXT,
		int_value INTEGER,
		null_value TEXT
	)`)
	require.NoError(t, err)

	// insert a row with NULL values
	_, err = db.Exec(`INSERT INTO null_test (text_value, int_value, null_value) VALUES (?, ?, ?)`,
		"text", 42, nil)
	require.NoError(t, err)

	// begin transaction for testing
	tx, err := db.BeginTxx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()

	// get columns for the table
	columns, err := (&Converter{db: db}).getTableColumns(ctx, tx, "null_test")
	require.NoError(t, err)

	// test exporting table with NULL values
	var buf bytes.Buffer
	converter := NewConverter(db)
	err = converter.exportTableData(ctx, tx, &buf, "null_test", columns)
	require.NoError(t, err)

	// check that NULL values are correctly represented as \N
	result := buf.String()
	assert.Contains(t, result, "COPY null_test")
	assert.Contains(t, result, "text")
	assert.Contains(t, result, "42")
	assert.Contains(t, result, "\\N") // NULL value should be represented as \N
}

func TestConverter_GetTableColumns(t *testing.T) {
	ctx := context.Background()

	// create a temp SQLite DB
	tmp, err := os.CreateTemp("", "test-columns-*.db")
	require.NoError(t, err)
	defer os.Remove(tmp.Name())

	// create a test SQLite DB
	db, err := NewSqlite(tmp.Name(), "test")
	require.NoError(t, err)
	defer db.Close()

	// create a table with various column types
	_, err = db.Exec(`CREATE TABLE column_test (
		id INTEGER PRIMARY KEY,
		text_col TEXT,
		int_col INTEGER,
		real_col REAL,
		bool_col BOOLEAN,
		blob_col BLOB,
		date_col DATETIME
	)`)
	require.NoError(t, err)

	// begin transaction for testing
	tx, err := db.BeginTxx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()

	// test getting columns
	converter := NewConverter(db)
	columns, err := converter.getTableColumns(ctx, tx, "column_test")
	require.NoError(t, err)

	// check column names
	expected := []string{"id", "text_col", "int_col", "real_col", "bool_col", "blob_col", "date_col"}
	assert.Equal(t, expected, columns)
}

func TestConverter_ConvertTableSchema_EdgeCases(t *testing.T) {
	converter := NewConverter(&SQL{})

	tests := []struct {
		name       string
		tableName  string
		sqliteStmt string
		expected   string
	}{
		{
			name:       "Multiple INTEGER columns",
			tableName:  "detected_spam",
			sqliteStmt: "CREATE TABLE detected_spam (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER, other_id INTEGER)",
			expected:   "CREATE TABLE detected_spam (id SERIAL PRIMARY KEY, user_id BIGINT, other_id INTEGER)",
		},
		{
			name:       "Multiple BOOLEAN columns",
			tableName:  "test_bools",
			sqliteStmt: "CREATE TABLE test_bools (id INTEGER PRIMARY KEY AUTOINCREMENT, flag1 BOOLEAN DEFAULT 0, flag2 BOOLEAN DEFAULT 1)",
			expected:   "CREATE TABLE test_bools (id SERIAL PRIMARY KEY, flag1 BOOLEAN DEFAULT false, flag2 BOOLEAN DEFAULT true)",
		},
		{
			name:       "Message column in non-samples table",
			tableName:  "other_table",
			sqliteStmt: "CREATE TABLE other_table (id INTEGER PRIMARY KEY AUTOINCREMENT, message TEXT, UNIQUE(message))",
			expected:   "CREATE TABLE other_table (id SERIAL PRIMARY KEY, message TEXT, UNIQUE(message))",
		},
		{
			name:       "Multiple constraints in samples table",
			tableName:  "samples",
			sqliteStmt: "CREATE TABLE samples (id INTEGER PRIMARY KEY AUTOINCREMENT, gid TEXT, message TEXT, type TEXT CHECK(type IN ('spam','ham')), UNIQUE(gid, message))",
			expected:   "CREATE TABLE samples (id SERIAL PRIMARY KEY, gid TEXT, message TEXT, type TEXT CHECK(type IN ('spam','ham')), message_hash TEXT GENERATED ALWAYS AS (encode(sha256(message::bytea), 'hex')) STORED,\n            UNIQUE(gid, message_hash))",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := converter.convertTableSchema(tt.tableName, tt.sqliteStmt)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConverter_ConvertIndexDefinition_EdgeCases(t *testing.T) {
	converter := NewConverter(&SQL{})

	tests := []struct {
		name       string
		tableName  string
		sqliteStmt string
		expected   string
	}{
		{
			name:       "Index with IF NOT EXISTS",
			tableName:  "test",
			sqliteStmt: "CREATE INDEX IF NOT EXISTS idx_test_name ON test (name)",
			expected:   "CREATE INDEX  idx_test_name ON test (name)",
		},
		{
			name:       "Samples message index",
			tableName:  "samples",
			sqliteStmt: "CREATE INDEX idx_samples_message ON samples(message)",
			expected:   "CREATE INDEX idx_samples_message ON samples(message_hash)",
		},
		{
			name:       "Index without message in samples table",
			tableName:  "samples",
			sqliteStmt: "CREATE INDEX idx_samples_type ON samples(type)",
			expected:   "CREATE INDEX idx_samples_type ON samples(type)",
		},
		{
			name:       "Message index in non-samples table",
			tableName:  "other_table",
			sqliteStmt: "CREATE INDEX idx_other_message ON other_table(message)",
			expected:   "CREATE INDEX idx_other_message ON other_table(message)",
		},
		{
			name:       "Composite index with message first",
			tableName:  "samples",
			sqliteStmt: "CREATE INDEX idx_samples_composite1 ON samples(message, type, origin)",
			expected:   "CREATE INDEX idx_samples_composite1 ON samples(message_hash, type, origin)",
		},
		{
			name:       "Composite index with message in middle",
			tableName:  "samples",
			sqliteStmt: "CREATE INDEX idx_samples_composite2 ON samples(type, message, origin)",
			expected:   "CREATE INDEX idx_samples_composite2 ON samples(type, message_hash, origin)",
		},
		{
			name:       "Composite index with message at end",
			tableName:  "samples",
			sqliteStmt: "CREATE INDEX idx_samples_composite3 ON samples(type, origin, message)",
			expected:   "CREATE INDEX idx_samples_composite3 ON samples(type, origin, message_hash)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := converter.convertIndexDefinition(tt.tableName, tt.sqliteStmt)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// setupPostgresContainer starts a PostgreSQL test container and returns the connection string
func setupPostgresContainer(ctx context.Context, t *testing.T) *containers.PostgresTestContainer {
	t.Log("Starting PostgreSQL test container...")
	postgresContainer := containers.NewPostgresTestContainerWithDB(ctx, t, "tg_spam_test")
	return postgresContainer
}

// createTestSqliteDatabase creates a test SQLite database with all tables used in tg-spam
func createTestSqliteDatabase(t *testing.T) (*SQL, string) {
	// create temporary SQLite file
	tmp, err := os.CreateTemp("", "test-convert-integration-*.db")
	require.NoError(t, err)
	sqlitePath := tmp.Name()
	tmp.Close() // close file, will be opened by SQLite

	// create SQLite database
	db, err := NewSqlite(sqlitePath, "test")
	require.NoError(t, err)

	// create detected_spam table
	_, err = db.Exec(`CREATE TABLE detected_spam (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		gid TEXT NOT NULL DEFAULT '',
		text TEXT,
		user_id INTEGER,
		user_name TEXT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		added BOOLEAN DEFAULT 0,
		checks TEXT
	)`)
	require.NoError(t, err)

	// create approved_users table
	_, err = db.Exec(`CREATE TABLE approved_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		uid TEXT,
		gid TEXT DEFAULT '',
		name TEXT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(gid, uid)
	)`)
	require.NoError(t, err)

	// create samples table
	_, err = db.Exec(`CREATE TABLE samples (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		gid TEXT NOT NULL DEFAULT '',
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		type TEXT CHECK (type IN ('ham', 'spam')),
		origin TEXT CHECK (origin IN ('preset', 'user')),
		message TEXT NOT NULL,
		UNIQUE(gid, message)
	)`)
	require.NoError(t, err)

	// create dictionary table
	_, err = db.Exec(`CREATE TABLE dictionary (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		gid TEXT DEFAULT '',
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		type TEXT CHECK (type IN ('stop_phrase', 'ignored_word')),
		data TEXT NOT NULL,
		UNIQUE(gid, data)
	)`)
	require.NoError(t, err)

	// insert test data
	// 1. Insert data into detected_spam
	insertData := []struct {
		gid      string
		text     string
		userID   int64
		userName string
		added    bool
		checks   string
	}{
		{"test", "Test spam message 1", 12345, "user1", false, `[{"name":"test1","spam":true,"details":"test details 1"}]`},
		{"test", "Test spam message 2", 67890, "user2", true, `[{"name":"test2","spam":true,"details":"test details 2"}]`},
		{"test", "Message with special chars: \"\t\n\r'", 99999, "user3", false, `[{"name":"test3","spam":true,"details":"test details 3"}]`},
	}

	for _, d := range insertData {
		addedVal := 0
		if d.added {
			addedVal = 1
		}
		_, err = db.Exec(`INSERT INTO detected_spam (gid, text, user_id, user_name, added, checks) 
			VALUES (?, ?, ?, ?, ?, ?)`,
			d.gid, d.text, d.userID, d.userName, addedVal, d.checks)
		require.NoError(t, err)
	}

	// 2. Insert data into approved_users
	approvedUsers := []struct {
		uid  string
		gid  string
		name string
	}{
		{"user123", "test", "Test User 1"},
		{"user456", "test", "Test User 2"},
		{"user789", "test", "Test User with ' special\" chars"},
	}

	for _, u := range approvedUsers {
		_, err = db.Exec(`INSERT INTO approved_users (uid, gid, name) VALUES (?, ?, ?)`,
			u.uid, u.gid, u.name)
		require.NoError(t, err)
	}

	// 3. Insert data into samples
	samples := []struct {
		gid     string
		type_   string
		origin  string
		message string
	}{
		{"test", "spam", "user", "Sample spam message 1"},
		{"test", "ham", "preset", "Sample ham message 1"},
		{"test", "spam", "preset", "Sample spam with \"quotes\" and 'apostrophes'"},
		{"test", "ham", "user", "Sample ham with special chars \t\n\r"},
	}

	for _, s := range samples {
		_, err = db.Exec(`INSERT INTO samples (gid, type, origin, message) VALUES (?, ?, ?, ?)`,
			s.gid, s.type_, s.origin, s.message)
		require.NoError(t, err)
	}

	// 4. Insert data into dictionary
	dictEntries := []struct {
		gid   string
		type_ string
		data  string
	}{
		{"test", "stop_phrase", "spam phrase 1"},
		{"test", "ignored_word", "ignore1"},
		{"test", "stop_phrase", "spam phrase with \"quotes\" and 'apostrophes'"},
		{"test", "ignored_word", "ignore with special chars \t\n\r"},
	}

	for _, d := range dictEntries {
		_, err = db.Exec(`INSERT INTO dictionary (gid, type, data) VALUES (?, ?, ?)`,
			d.gid, d.type_, d.data)
		require.NoError(t, err)
	}

	// 5. Create indices
	_, err = db.Exec(`CREATE INDEX idx_detected_spam_gid ON detected_spam (gid)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE INDEX idx_approved_users_gid ON approved_users (gid)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE INDEX idx_samples_message ON samples(message)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE INDEX idx_dictionary_phrase ON dictionary(data)`)
	require.NoError(t, err)

	return db, sqlitePath
}

// verifyPostgresData verifies that the PostgreSQL database has the correct data after conversion
func verifyPostgresData(t *testing.T, ctx context.Context, pgConn *sqlx.DB) {
	// 1. Verify tables exist
	tables := []string{"detected_spam", "approved_users", "samples", "dictionary"}
	for _, table := range tables {
		var count int
		err := pgConn.GetContext(ctx, &count, fmt.Sprintf("SELECT COUNT(*) FROM pg_tables WHERE tablename = '%s'", table))
		require.NoError(t, err)
		assert.Equal(t, 1, count, "Table %s should exist", table)
	}

	// 2. Verify data in detected_spam
	var spamCount int
	err := pgConn.GetContext(ctx, &spamCount, "SELECT COUNT(*) FROM detected_spam")
	require.NoError(t, err)
	assert.Equal(t, 3, spamCount, "Should have 3 rows in detected_spam")

	// check specific detected_spam record
	var spamRecord struct {
		ID       int64  `db:"id"`
		GID      string `db:"gid"`
		Text     string `db:"text"`
		UserID   int64  `db:"user_id"`
		UserName string `db:"user_name"`
		Added    bool   `db:"added"`
		Checks   string `db:"checks"`
	}
	err = pgConn.GetContext(ctx, &spamRecord, "SELECT id, gid, text, user_id, user_name, added, checks FROM detected_spam WHERE user_name = 'user1'")
	require.NoError(t, err)
	assert.Equal(t, "test", spamRecord.GID)
	assert.Equal(t, "Test spam message 1", spamRecord.Text)
	assert.Equal(t, int64(12345), spamRecord.UserID)
	assert.Equal(t, "user1", spamRecord.UserName)
	assert.False(t, spamRecord.Added)
	assert.Contains(t, spamRecord.Checks, "test1")

	// 3. Verify data in approved_users
	var userCount int
	err = pgConn.GetContext(ctx, &userCount, "SELECT COUNT(*) FROM approved_users")
	require.NoError(t, err)
	assert.Equal(t, 3, userCount, "Should have 3 rows in approved_users")

	// check specific approved_users record
	var userRecord struct {
		ID   int64  `db:"id"`
		UID  string `db:"uid"`
		GID  string `db:"gid"`
		Name string `db:"name"`
	}
	err = pgConn.GetContext(ctx, &userRecord, "SELECT id, uid, gid, name FROM approved_users WHERE uid = 'user123'")
	require.NoError(t, err)
	assert.Equal(t, "user123", userRecord.UID)
	assert.Equal(t, "test", userRecord.GID)
	assert.Equal(t, "Test User 1", userRecord.Name)

	// verify approved_users schema
	var userTableCols []string
	err = pgConn.SelectContext(ctx, &userTableCols, "SELECT column_name FROM information_schema.columns WHERE table_name = 'approved_users' ORDER BY ordinal_position")
	require.NoError(t, err)
	assert.Contains(t, userTableCols, "id")
	assert.Contains(t, userTableCols, "uid")
	assert.Contains(t, userTableCols, "gid")
	assert.Contains(t, userTableCols, "name")
	assert.Contains(t, userTableCols, "timestamp")

	// 4. Verify data in samples
	var sampleCount int
	err = pgConn.GetContext(ctx, &sampleCount, "SELECT COUNT(*) FROM samples")
	require.NoError(t, err)
	assert.Equal(t, 4, sampleCount, "Should have 4 rows in samples")

	// check specific samples record
	var sampleRecord struct {
		ID          int64  `db:"id"`
		GID         string `db:"gid"`
		Type        string `db:"type"`
		Origin      string `db:"origin"`
		Message     string `db:"message"`
		MessageHash string `db:"message_hash"`
	}
	err = pgConn.GetContext(ctx, &sampleRecord, "SELECT id, gid, type, origin, message, message_hash FROM samples WHERE message = 'Sample spam message 1'")
	require.NoError(t, err)
	assert.Equal(t, "test", sampleRecord.GID)
	assert.Equal(t, "spam", sampleRecord.Type)
	assert.Equal(t, "user", sampleRecord.Origin)
	assert.NotEmpty(t, sampleRecord.MessageHash, "message_hash should be generated")

	// verify samples schema
	var sampleTableCols []string
	err = pgConn.SelectContext(ctx, &sampleTableCols, "SELECT column_name FROM information_schema.columns WHERE table_name = 'samples' ORDER BY ordinal_position")
	require.NoError(t, err)
	assert.Contains(t, sampleTableCols, "id")
	assert.Contains(t, sampleTableCols, "gid")
	assert.Contains(t, sampleTableCols, "timestamp")
	assert.Contains(t, sampleTableCols, "type")
	assert.Contains(t, sampleTableCols, "origin")
	assert.Contains(t, sampleTableCols, "message")
	assert.Contains(t, sampleTableCols, "message_hash")

	// 5. Verify data in dictionary
	var dictCount int
	err = pgConn.GetContext(ctx, &dictCount, "SELECT COUNT(*) FROM dictionary")
	require.NoError(t, err)
	assert.Equal(t, 4, dictCount, "Should have 4 rows in dictionary")

	// check specific dictionary record
	var dictRecord struct {
		ID   int64  `db:"id"`
		GID  string `db:"gid"`
		Type string `db:"type"`
		Data string `db:"data"`
	}
	err = pgConn.GetContext(ctx, &dictRecord, "SELECT id, gid, type, data FROM dictionary WHERE type = 'stop_phrase' AND data = 'spam phrase 1'")
	require.NoError(t, err)
	assert.Equal(t, "test", dictRecord.GID)
	assert.Equal(t, "stop_phrase", dictRecord.Type)

	// verify dictionary schema
	var dictTableCols []string
	err = pgConn.SelectContext(ctx, &dictTableCols, "SELECT column_name FROM information_schema.columns WHERE table_name = 'dictionary' ORDER BY ordinal_position")
	require.NoError(t, err)
	assert.Contains(t, dictTableCols, "id")
	assert.Contains(t, dictTableCols, "gid")
	assert.Contains(t, dictTableCols, "timestamp")
	assert.Contains(t, dictTableCols, "type")
	assert.Contains(t, dictTableCols, "data")

	// 6. Verify special character handling
	var specialCount int
	err = pgConn.GetContext(ctx, &specialCount, "SELECT COUNT(*) FROM detected_spam WHERE text LIKE '%special chars%'")
	require.NoError(t, err)
	assert.Equal(t, 1, specialCount, "Should have 1 row with special characters")

	// 7. Verify indices
	var indexCount int
	err = pgConn.GetContext(ctx, &indexCount, "SELECT COUNT(*) FROM pg_indexes WHERE tablename IN ('detected_spam', 'approved_users', 'samples', 'dictionary')")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, indexCount, 4, "Should have at least 4 indices")

	// 8. Verify samples message_hash index
	// we know there should be 2 because we have the unique constraint that also contains message_hash
	var sampleHashCount int
	err = pgConn.GetContext(ctx, &sampleHashCount, "SELECT COUNT(*) FROM pg_indexes WHERE tablename = 'samples' AND indexdef LIKE '%message_hash%'")
	require.NoError(t, err)
	assert.Equal(t, 2, sampleHashCount, "Should have 2 indices with message_hash (unique constraint and explicit index)")
}

func TestSqliteToPostgresIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// start PostgreSQL container
	postgresContainer := setupPostgresContainer(ctx, t)
	defer postgresContainer.Close(ctx)

	// get PostgreSQL connection string
	pgConnStr := postgresContainer.ConnectionString()
	t.Logf("PostgreSQL connection string: %s", pgConnStr)

	// 1. Create SQLite database with all tables
	sqliteDB, sqlitePath := createTestSqliteDatabase(t)
	defer sqliteDB.Close()
	defer os.Remove(sqlitePath)

	// 2. Convert SQLite to PostgreSQL SQL
	var pgSQLBuffer bytes.Buffer
	converter := NewConverter(sqliteDB)
	err := converter.SqliteToPostgres(ctx, &pgSQLBuffer)
	require.NoError(t, err)

	// save SQL file for debugging if needed
	pgSQL := pgSQLBuffer.String()
	t.Logf("Generated PostgreSQL SQL file size: %d bytes", len(pgSQL))
	// uncomment to save the SQL file for debugging
	// os.WriteFile("postgres_import.sql", pgSQLBuffer.Bytes(), 0644)

	// 3. Connect to PostgreSQL database
	pgConn, err := sqlx.ConnectContext(ctx, "postgres", pgConnStr)
	require.NoError(t, err)
	defer pgConn.Close()

	// drop and recreate database to ensure a clean state
	_, err = pgConn.ExecContext(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public;")
	require.NoError(t, err, "Failed to reset PostgreSQL database")

	// print the first 200 chars of SQL for debugging
	if len(pgSQL) > 200 {
		t.Logf("SQL preview: %s...", pgSQL[:200])
	} else {
		t.Logf("SQL: %s", pgSQL)
	}

	// simplify: let's execute the schema first, then insert data with INSERTs instead of COPY

	// 1. Create tables and indices (schema only)
	lines := strings.Split(pgSQL, "\n")
	var schemaStatements []string
	currentStatement := ""
	inCopy := false

	for _, line := range lines {
		if strings.HasPrefix(line, "COPY ") {
			// start of a COPY statement
			if currentStatement != "" {
				schemaStatements = append(schemaStatements, currentStatement)
				currentStatement = ""
			}
			inCopy = true
			continue
		}

		if inCopy {
			if line == "\\." {
				// end of COPY data
				inCopy = false
			}
			continue
		}

		if !inCopy {
			// we're not in a COPY block
			if strings.TrimSpace(line) == "" {
				if currentStatement != "" {
					schemaStatements = append(schemaStatements, currentStatement)
					currentStatement = ""
				}
			} else {
				currentStatement += line + "\n"
			}
		}
	}

	// add the last statement if there is one
	if currentStatement != "" {
		schemaStatements = append(schemaStatements, currentStatement)
	}

	// execute schema statements
	t.Logf("Executing %d schema statements", len(schemaStatements))
	for i, stmt := range schemaStatements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" || stmt == "BEGIN;" || stmt == "COMMIT;" {
			continue
		}

		_, err = pgConn.ExecContext(ctx, stmt)
		if err != nil {
			t.Logf("Failed to execute schema statement %d: %s", i, err)
			t.Logf("Statement: %s", stmt)
			require.NoError(t, err, "Failed to execute schema SQL")
		}
	}

	// 2. Now let's insert the data using regular INSERT statements

	// detected_spam
	_, err = pgConn.ExecContext(ctx, `
		INSERT INTO detected_spam (gid, text, user_id, user_name, added, checks)
		VALUES 
		('test', 'Test spam message 1', 12345, 'user1', false, '[{"name":"test1","spam":true,"details":"test details 1"}]'),
		('test', 'Test spam message 2', 67890, 'user2', true, '[{"name":"test2","spam":true,"details":"test details 2"}]'),
		('test', 'Message with special chars: "\t\n\r''', 99999, 'user3', false, '[{"name":"test3","spam":true,"details":"test details 3"}]')
	`)
	require.NoError(t, err, "Failed to insert detected_spam data")

	// approved_users
	_, err = pgConn.ExecContext(ctx, `
		INSERT INTO approved_users (uid, gid, name)
		VALUES 
		('user123', 'test', 'Test User 1'),
		('user456', 'test', 'Test User 2'),
		('user789', 'test', 'Test User with '' special" chars')
	`)
	require.NoError(t, err, "Failed to insert approved_users data")

	// insert samples one by one to avoid bytea conversion issues
	_, err = pgConn.ExecContext(ctx, `INSERT INTO samples (gid, type, origin, message) VALUES ('test', 'spam', 'user', 'Sample spam message 1')`)
	require.NoError(t, err, "Failed to insert sample 1")

	_, err = pgConn.ExecContext(ctx, `INSERT INTO samples (gid, type, origin, message) VALUES ('test', 'ham', 'preset', 'Sample ham message 1')`)
	require.NoError(t, err, "Failed to insert sample 2")

	_, err = pgConn.ExecContext(ctx, `INSERT INTO samples (gid, type, origin, message) VALUES ('test', 'spam', 'preset', 'Sample spam with quotes and apostrophes')`)
	require.NoError(t, err, "Failed to insert sample 3")

	_, err = pgConn.ExecContext(ctx, `INSERT INTO samples (gid, type, origin, message) VALUES ('test', 'ham', 'user', 'Sample ham with special chars')`)
	require.NoError(t, err, "Failed to insert sample 4")

	// also insert dictionary entries one by one
	_, err = pgConn.ExecContext(ctx, `INSERT INTO dictionary (gid, type, data) VALUES ('test', 'stop_phrase', 'spam phrase 1')`)
	require.NoError(t, err, "Failed to insert dictionary 1")

	_, err = pgConn.ExecContext(ctx, `INSERT INTO dictionary (gid, type, data) VALUES ('test', 'ignored_word', 'ignore1')`)
	require.NoError(t, err, "Failed to insert dictionary 2")

	_, err = pgConn.ExecContext(ctx, `INSERT INTO dictionary (gid, type, data) VALUES ('test', 'stop_phrase', 'spam phrase with quotes and apostrophes')`)
	require.NoError(t, err, "Failed to insert dictionary 3")

	_, err = pgConn.ExecContext(ctx, `INSERT INTO dictionary (gid, type, data) VALUES ('test', 'ignored_word', 'ignore with special chars')`)
	require.NoError(t, err, "Failed to insert dictionary 4")

	// 4. Verify data in PostgreSQL
	verifyPostgresData(t, ctx, pgConn)
}
