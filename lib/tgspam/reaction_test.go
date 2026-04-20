package tgspam

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReactionDetector(t *testing.T) {
	t.Run("disabled when threshold is zero", func(t *testing.T) {
		d := newReactionDetector(0, time.Hour)
		require.Nil(t, d)
		var nd *reactionDetector
		resp := nd.check(123)
		assert.False(t, resp.Spam)
		assert.Equal(t, "reactions", resp.Name)
		assert.Equal(t, "disabled", resp.Details)
	})

	t.Run("disabled when threshold is negative", func(t *testing.T) {
		d := newReactionDetector(-1, time.Hour)
		assert.Nil(t, d)
	})

	t.Run("below threshold", func(t *testing.T) {
		d := newReactionDetector(3, time.Hour)
		require.NotNil(t, d)
		resp := d.check(1)
		assert.False(t, resp.Spam)
		resp = d.check(1)
		assert.False(t, resp.Spam)
	})

	t.Run("threshold reached", func(t *testing.T) {
		d := newReactionDetector(3, time.Hour)
		require.NotNil(t, d)
		d.check(1)
		d.check(1)
		resp := d.check(1)
		assert.True(t, resp.Spam)
		assert.Equal(t, "reactions", resp.Name)
	})

	t.Run("threshold exceeded on subsequent calls", func(t *testing.T) {
		d := newReactionDetector(2, time.Hour)
		require.NotNil(t, d)
		d.check(1)
		d.check(1)
		resp := d.check(1)
		assert.True(t, resp.Spam)
	})

	t.Run("independent users", func(t *testing.T) {
		d := newReactionDetector(3, time.Hour)
		require.NotNil(t, d)
		d.check(1)
		d.check(1)
		d.check(1) // user 1 at threshold

		resp2 := d.check(2) // user 2 first reaction
		assert.False(t, resp2.Spam)
	})

	t.Run("window expiry resets counter", func(t *testing.T) {
		d := newReactionDetector(2, 50*time.Millisecond)
		require.NotNil(t, d)
		d.check(1)
		d.check(1) // at threshold

		// wait for window to expire
		time.Sleep(60 * time.Millisecond)

		resp := d.check(1) // should reset
		assert.False(t, resp.Spam, "counter should have been reset after window expiry")
	})
}
