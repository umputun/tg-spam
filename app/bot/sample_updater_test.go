package bot

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSampleUpdater(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		file, err := os.CreateTemp(os.TempDir(), "sample")
		require.NoError(t, err)
		defer os.Remove(file.Name())

		updater := newSampleUpdater(file.Name())
		err = updater.append("Test message")
		assert.NoError(t, err)

		reader, err := updater.reader()
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

		updater := newSampleUpdater(file.Name())
		err = updater.append("Test message\nsecond line\nthird line")
		assert.NoError(t, err)

		reader, err := updater.reader()
		require.NoError(t, err)
		defer reader.Close()

		content, err := io.ReadAll(reader)
		assert.NoError(t, err)
		assert.Equal(t, "Test message second line third line\n", string(content))
	})

	t.Run("unhappy path", func(t *testing.T) {
		updater := newSampleUpdater("/tmp/non-existent/samples.txt")
		err := updater.append("Test message")
		assert.Error(t, err)
		_, err = updater.reader()
		assert.Error(t, err)
	})
}
