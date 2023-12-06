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

	time.AfterFunc(time.Millisecond*500, func() {
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

func TestWatchPair_bothFilesChanged(t *testing.T) {
	// Create two temporary files
	tmpfile1, err := os.CreateTemp("", "watcher1")
	require.NoError(t, err)
	defer os.Remove(tmpfile1.Name()) // clean up

	tmpfile2, err := os.CreateTemp("", "watcher2")
	require.NoError(t, err)
	defer os.Remove(tmpfile2.Name()) // clean up

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dataChangeCalled := false
	var dataChangeContent1, dataChangeContent2 string
	onDataChange := func(r1, r2 io.Reader) error {
		dataChangeCalled = true
		data1, e := io.ReadAll(r1)
		if e != nil {
			return e
		}
		dataChangeContent1 = string(data1)

		data2, e := io.ReadAll(r2)
		if e != nil {
			return e
		}
		dataChangeContent2 = string(data2)
		return nil
	}

	time.AfterFunc(time.Millisecond*500, func() {
		_, err = tmpfile1.WriteString("hello world 1")
		require.NoError(t, err)
		tmpfile1.Close()

		_, err = tmpfile2.WriteString("hello world 2")
		require.NoError(t, err)
		tmpfile2.Close()

		cancel()
	})

	watchPair(ctx, tmpfile1.Name(), tmpfile2.Name(), onDataChange)
	assert.True(t, dataChangeCalled, "onDataChange should have been called")
	assert.Equal(t, "hello world 1", dataChangeContent1, "onDataChange should have received the correct data from file 1")
	assert.Equal(t, "hello world 2", dataChangeContent2, "onDataChange should have received the correct data from file 2")
}

func TestWatchPair_oneFileChanged(t *testing.T) {
	// Create two temporary files
	tmpfile1, err := os.CreateTemp("", "watcher1")
	require.NoError(t, err)
	defer os.Remove(tmpfile1.Name()) // clean up

	tmpfile2, err := os.CreateTemp("", "watcher2")
	require.NoError(t, err)
	defer os.Remove(tmpfile2.Name()) // clean up

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dataChangeCalled := false
	var dataChangeContent1, dataChangeContent2 string
	onDataChange := func(r1, r2 io.Reader) error {
		dataChangeCalled = true
		data1, e := io.ReadAll(r1)
		if e != nil {
			return e
		}
		dataChangeContent1 = string(data1)

		data2, e := io.ReadAll(r2)
		if e != nil {
			return e
		}
		dataChangeContent2 = string(data2)
		return nil
	}

	time.AfterFunc(time.Millisecond*900, func() {
		_, err = tmpfile1.WriteString("hello world 1")
		require.NoError(t, err)
		tmpfile1.Close()
		// do not write to tmpfile2
		cancel()
	})

	watchPair(ctx, tmpfile1.Name(), tmpfile2.Name(), onDataChange)
	assert.True(t, dataChangeCalled, "onDataChange should have been called")
	assert.Equal(t, "hello world 1", dataChangeContent1, "onDataChange should have received the correct data from file 1")
	assert.Equal(t, "", dataChangeContent2, "onDataChange should have received no data from file 2 because it was not changed")
}
