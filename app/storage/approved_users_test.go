package storage

import (
	"bytes"
	"io"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApprovedUsers_StoreAndRead(t *testing.T) {
	tests := []struct {
		name     string
		ids      []int64
		expected []int64
	}{
		{
			name:     "empty",
			ids:      []int64{},
			expected: []int64{},
		},
		{
			name:     "single ID",
			ids:      []int64{12345},
			expected: []int64{12345},
		},
		{
			name:     "multiple IDs",
			ids:      []int64{123, 456, 789},
			expected: []int64{123, 456, 789},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory
			tmpDir, err := os.MkdirTemp("", "test_approved_users")
			require.NoError(t, err)
			defer os.RemoveAll(tmpDir) // Clean up

			// Create a file path in the temporary directory
			filePath := tmpDir + "/testfile.bin"

			// Initialize ApprovedUsers with the temporary file path
			au := NewApprovedUsers(filePath)

			// Store the IDs
			err = au.Store(tt.ids)
			require.NoError(t, err)

			// Read the IDs back
			var readBuffer bytes.Buffer
			buf := make([]byte, 1024) // Buffer for reading
			for {
				n, err := au.Read(buf)
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
				readBuffer.Write(buf[:n])
			}

			// Convert read data back to IDs
			var readIDs []int64
			if readBuffer.Len() > 0 {
				for _, strID := range bytes.Split(readBuffer.Bytes(), []byte("\n")) {
					if len(strID) == 0 {
						continue
					}
					id, err := strconv.ParseInt(string(strID), 10, 64)
					require.NoError(t, err)
					readIDs = append(readIDs, id)
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

func TestApprovedUsers_StoreFailure(t *testing.T) {
	au := NewApprovedUsers("/invalid/path/testfile.bin")
	// attempt to store IDs to an invalid path
	err := au.Store([]int64{12345})
	assert.Error(t, err, "store should fail with an invalid file path")
}

func TestApprovedUsers_ReadFailure(t *testing.T) {
	au := NewApprovedUsers("/invalid/path/testfile.bin")
	// attempt to read from a non-existent file
	buf := make([]byte, 1024)
	_, err := au.Read(buf)
	assert.Error(t, err, "read should fail with a non-existent file")
}
