package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPickQuery(t *testing.T) {
	qmap := QueryMap{
		Sqlite: {
			1: "SELECT * FROM test WHERE id = ?",
			2: "INSERT INTO test VALUES (?)",
		},
		Postgres: {
			1: "SELECT * FROM test WHERE id = $1",
			2: "INSERT INTO test VALUES ($1)",
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
			got, err := PickQuery(qmap, tt.dbType, tt.cmd)
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
