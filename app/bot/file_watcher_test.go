package bot

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWatch(t *testing.T) {
	// Create a temporary file
	tmpfile, err := os.CreateTemp("", "watcher")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name()) // clean up

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dataChangeCalled := false
	var dataChangeContent string
	onDataChange := func(r io.Reader) error {
		dataChangeCalled = true
		data, e := io.ReadAll(r)
		if e != nil {
			return e
		}
		dataChangeContent = string(data)
		return nil
	}

	time.AfterFunc(time.Millisecond*100, func() {
		time.Sleep(time.Millisecond * 100)
		_, err = tmpfile.WriteString("hello world")
		require.NoError(t, err)
		tmpfile.Close()
		cancel()
	})

	err = watch(ctx, tmpfile.Name(), onDataChange)
	assert.NoError(t, err)
	assert.True(t, dataChangeCalled, "onDataChange should have been called")
	assert.Equal(t, "hello world", dataChangeContent, "onDataChange should have received the correct data")
}
