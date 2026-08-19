package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedactConnURL(t *testing.T) {
	tbl := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"sqlite file name", "tg-spam.db", "tg-spam.db"},
		{"sqlite path", "/var/lib/tg-spam/tg-spam.db", "/var/lib/tg-spam/tg-spam.db"},
		{"sqlite file url", "file:///var/lib/tg-spam.db", "file:///var/lib/tg-spam.db"},
		{"memory", ":memory:", ":memory:"},
		{"postgres with password", "postgres://user:s3cret@localhost:5432/spam", "postgres://user:xxxxx@localhost:5432/spam"},
		{"postgres without password", "postgres://user@localhost:5432/spam", "postgres://user@localhost:5432/spam"},
		{"postgres without userinfo", "postgres://localhost:5432/spam", "postgres://localhost:5432/spam"},
		{"postgres with empty password", "postgres://user:@localhost:5432/spam", "postgres://user:xxxxx@localhost:5432/spam"},
		{"postgres with encoded password", "postgres://user:p%40ss%3Aword@localhost/spam", "postgres://user:xxxxx@localhost/spam"},
		{"postgres with password query param", "postgres://localhost/spam?password=s3cret&sslmode=disable",
			"postgres://localhost/spam?password=xxxxx&sslmode=disable"},
		{"postgres with uppercase password query param", "postgres://localhost/spam?PassWord=s3cret",
			"postgres://localhost/spam?PassWord=xxxxx"},
		{"postgres with both", "postgres://user:s3cret@localhost/spam?sslpassword=other",
			"postgres://user:xxxxx@localhost/spam?sslpassword=xxxxx"},
		{"postgres query with no secrets untouched", "postgres://localhost/spam?sslmode=require&x=1",
			"postgres://localhost/spam?sslmode=require&x=1"},
		{"mysql dsn", "user:s3cret@tcp(localhost:3306)/spam", "user:xxxxx@tcp(localhost:3306)/spam"},
		{"mysql dsn without password", "user@tcp(localhost:3306)/spam", "user@tcp(localhost:3306)/spam"},
		{"malformed url keeps only the scheme", "postgres://user:s3\ncret@localhost/spam", "postgres://xxxxx"},
	}

	for _, tt := range tbl {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactConnURL(tt.in)
			assert.Equal(t, tt.want, got)
			assert.NotContains(t, got, "s3cret")
			assert.NotContains(t, got, "p@ss:word")
			assert.NotContains(t, got, "p%40ss", "the percent-encoded password must go too")
		})
	}
}

func TestNew_errorDoesNotLeakCredentials(t *testing.T) {
	_, err := New(t.Context(), "mysql://user:s3cret@localhost:3306/spam", "gr1")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "s3cret")
}

func TestNewPostgres_invalidURLDoesNotLeakCredentials(t *testing.T) {
	_, err := NewPostgres(t.Context(), "postgres://user:s3\ncret@localhost/spam", "gr1")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "s3cret")
}
