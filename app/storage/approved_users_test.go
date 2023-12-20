package storage

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApprovedUsers_StoreAndRead(t *testing.T) {
	tests := []struct {
		name     string
		ids      []string
		expected []string
	}{
		{
			name:     "empty",
			ids:      []string{},
			expected: []string{},
		},
		{
			name:     "single ID",
			ids:      []string{"12345"},
			expected: []string{"12345"},
		},
		{
			name:     "multiple IDs",
			ids:      []string{"123", "456", "789"},
			expected: []string{"123", "456", "789"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "test_approved_users")
			require.NoError(t, err)
			defer os.RemoveAll(tmpDir) // Clean up

			filePath := tmpDir + "/testfile.bin"
			db, err := NewSqliteDB(filePath)
			require.NoError(t, err)
			au, err := NewApprovedUsers(db)
			require.NoError(t, err)

			err = au.Store(tt.ids)
			require.NoError(t, err)

			var readBuffer bytes.Buffer
			buf := make([]byte, 1024) // buffer for reading
			for {
				n, err := au.Read(buf)
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
				readBuffer.Write(buf[:n])
			}

			// convert read data back to IDs
			var readIDs []string
			if readBuffer.Len() > 0 {
				for _, strID := range strings.Split(readBuffer.String(), "\n") {
					if strID == "" {
						continue
					}
					readIDs = append(readIDs, strID)
				}
			}

			// Compare the original and read IDs, treating nil and empty slices as equal
			if len(tt.expected) == 0 {
				assert.Equal(t, 0, len(readIDs))
			} else {
				assert.Equal(t, tt.expected, readIDs)
			}
		})
	}
}
