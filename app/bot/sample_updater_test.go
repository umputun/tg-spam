package bot

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSampleUpdater(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		file, err := os.CreateTemp(os.TempDir(), "sample")
		require.NoError(t, err)
		defer os.Remove(file.Name())

		updater := NewSampleUpdater(file.Name())
		err = updater.Append("Test message")
		assert.NoError(t, err)

		reader, err := updater.Reader()
		require.NoError(t, err)
		defer reader.Close()

		content, err := io.ReadAll(reader)
		assert.NoError(t, err)
		assert.Equal(t, "Test message\n", string(content))
	})

	t.Run("dedup", func(t *testing.T) {
		file, err := os.CreateTemp(os.TempDir(), "sample")
		require.NoError(t, err)
		defer os.Remove(file.Name())

		updater := NewSampleUpdater(file.Name())
		err = updater.Append("Test message")
		assert.NoError(t, err)

		// duplicate message
		err = updater.Append(" Test message  ")
		assert.NoError(t, err)

		// duplicate message
		err = updater.Append(" TesT MessagE")
		assert.NoError(t, err)

		reader, err := updater.Reader()
		require.NoError(t, err)
		defer reader.Close()

		content, err := io.ReadAll(reader)
		assert.NoError(t, err)
		assert.Equal(t, "Test message\n", string(content))
	})

	t.Run("multi-line", func(t *testing.T) {
		file, err := os.CreateTemp(os.TempDir(), "sample")
		require.NoError(t, err)
		defer os.Remove(file.Name())

		updater := NewSampleUpdater(file.Name())
		err = updater.Append("Test message\nsecond line\nthird line")
		assert.NoError(t, err)

		reader, err := updater.Reader()
		require.NoError(t, err)
		defer reader.Close()

		content, err := io.ReadAll(reader)
		assert.NoError(t, err)
		assert.Equal(t, "Test message second line third line\n", string(content))
	})

	t.Run("no file", func(t *testing.T) {
		dir := os.TempDir()
		file := filepath.Join(dir, "non-existent-samples.txt")
		defer os.Remove(file)

		updater := NewSampleUpdater(file)
		err := updater.Append("Test message")
		assert.NoError(t, err)

		reader, err := updater.Reader()
		require.NoError(t, err)
		defer reader.Close()

		content, err := io.ReadAll(reader)
		assert.NoError(t, err)
		assert.Equal(t, "Test message\n", string(content))
	})

	t.Run("unhappy path", func(t *testing.T) {
		updater := NewSampleUpdater("/tmp/non-existent/samples.txt")
		err := updater.Append("Test message")
		assert.Error(t, err)
		_, err = updater.Reader()
		assert.Error(t, err)
	})
}
