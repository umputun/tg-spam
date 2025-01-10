package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPickQuery(t *testing.T) {
	mockProvider := &mockQueryProvider{
		qmap: QueryMap{
			Sqlite: {
				1: "SELECT * FROM test WHERE id = ?",
				2: "INSERT INTO test VALUES (?)",
			},
			Postgres: {
				1: "SELECT * FROM test WHERE id = $1",
				2: "INSERT INTO test VALUES ($1)",
			},
		},
	}

	tests := []struct {
		name    string
		dbType  Type
		cmd     DBCmd
		want    string
		wantErr bool
	}{
		{
			name:   "sqlite select",
			dbType: Sqlite,
			cmd:    1,
			want:   "SELECT * FROM test WHERE id = ?",
		},
		{
			name:   "postgres select",
			dbType: Postgres,
			cmd:    1,
			want:   "SELECT * FROM test WHERE id = $1",
		},
		{
			name:    "unknown db type",
			dbType:  Unknown,
			cmd:     1,
			wantErr: true,
		},
		{
			name:    "unknown command",
			dbType:  Sqlite,
			cmd:     99,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PickQuery(mockProvider, tt.dbType, tt.cmd)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

type mockQueryProvider struct {
	qmap QueryMap
}

func (m *mockQueryProvider) Map() QueryMap {
	return m.qmap
}

func Test_adoptQuery(t *testing.T) {
	tests := []struct {
		name     string
		dbType   Type
		query    string
		expected string
	}{
		{
			name:     "sqlite simple query",
			dbType:   Sqlite,
			query:    "SELECT * FROM test WHERE id = ?",
			expected: "SELECT * FROM test WHERE id = ?",
		},
		{
			name:     "sqlite multiple placeholders",
			dbType:   Sqlite,
			query:    "INSERT INTO test (id, name) VALUES (?, ?)",
			expected: "INSERT INTO test (id, name) VALUES (?, ?)",
		},
		{
			name:     "postgres simple query",
			dbType:   Postgres,
			query:    "SELECT * FROM test WHERE id = ?",
			expected: "SELECT * FROM test WHERE id = $1",
		},
		{
			name:     "postgres multiple placeholders",
			dbType:   Postgres,
			query:    "INSERT INTO test (id, name) VALUES (?, ?)",
			expected: "INSERT INTO test (id, name) VALUES ($1, $2)",
		},
		{
			name:     "postgres complex query",
			dbType:   Postgres,
			query:    "SELECT * FROM test WHERE id = ? AND name = ? OR value = ?",
			expected: "SELECT * FROM test WHERE id = $1 AND name = $2 OR value = $3",
		},
		{
			name:     "no placeholders",
			dbType:   Postgres,
			query:    "SELECT * FROM test",
			expected: "SELECT * FROM test",
		},
		{
			name:     "question mark in string literal",
			dbType:   Postgres,
			query:    "SELECT * FROM test WHERE text = '?' AND id = ?",
			expected: "SELECT * FROM test WHERE text = '?' AND id = $1",
		},
		{
			name:     "empty query",
			dbType:   Postgres,
			query:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &SQL{dbType: tt.dbType}
			result := e.AQ(tt.query)
			assert.Equal(t, tt.expected, result)
		})
	}

	t.Run("unknown type defaults to non-postgres", func(t *testing.T) {
		e := &SQL{dbType: Unknown}
		query := "SELECT * FROM test WHERE id = ?"
		result := e.AQ(query)
		assert.Equal(t, query, result)
	})
}
