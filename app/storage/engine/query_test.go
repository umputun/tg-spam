package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryMap(t *testing.T) {
	qmap := NewQueryMap().
		Add(1, Query{
			Sqlite:   "SELECT * FROM test WHERE id = ?",
			Postgres: "SELECT * FROM test WHERE id = $1",
		}).
		Add(2, Query{
			Sqlite:   "INSERT INTO test VALUES (?)",
			Postgres: "INSERT INTO test VALUES ($1)",
		})

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
			got, err := qmap.Pick(tt.dbType, tt.cmd)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestQueryMap_AddSame(t *testing.T) {
	qmap := NewQueryMap().
		AddSame(1, "SELECT * FROM test")

	sqliteQuery, err := qmap.Pick(Sqlite, 1)
	require.NoError(t, err)
	pgQuery, err := qmap.Pick(Postgres, 1)
	require.NoError(t, err)

	assert.Equal(t, "SELECT * FROM test", sqliteQuery)
	assert.Equal(t, "SELECT * FROM test", pgQuery)
}

func TestQueryMap_ChainedOperations(t *testing.T) {
	// test method chaining and overwriting
	qmap := NewQueryMap().
		Add(1, Query{
			Sqlite:   "query1 sqlite",
			Postgres: "query1 postgres",
		}).
		Add(1, Query{ // overwrite previous query
			Sqlite:   "query1 sqlite new",
			Postgres: "query1 postgres new",
		})

	query, err := qmap.Pick(Sqlite, 1)
	assert.NoError(t, err)
	assert.Equal(t, "query1 sqlite new", query)
}

func TestQueryMap_EmptyQueries(t *testing.T) {
	qmap := NewQueryMap().
		Add(1, Query{
			Sqlite:   "", // empty sqlite query
			Postgres: "not empty",
		})

	// empty queries are valid
	query, err := qmap.Pick(Sqlite, 1)
	assert.NoError(t, err)
	assert.Empty(t, query)
}

// This test verifies that AddSame maintains consistency between dialects
func TestQueryMap_AddSameConsistency(t *testing.T) {
	query := "SELECT * FROM test"
	qmap := NewQueryMap().
		AddSame(1, query).
		Add(1, Query{ // try to overwrite just one dialect
			Sqlite:   "different query",
			Postgres: "another query",
		}).
		AddSame(1, query) // restore consistency

	sqlite, err := qmap.Pick(Sqlite, 1)
	assert.NoError(t, err)
	postgres, err := qmap.Pick(Postgres, 1)
	assert.NoError(t, err)

	// verify both dialects have the same query after AddSame
	assert.Equal(t, query, sqlite)
	assert.Equal(t, query, postgres)
	assert.Equal(t, sqlite, postgres)
}
