package spamcheck

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLastRequestsBasicOps(t *testing.T) {
	h := NewLastRequests(5)
	req1 := Request{Msg: "msg1", UserID: "1", Meta: MetaData{Links: 1}}
	req2 := Request{Msg: "msg2", UserID: "2", Meta: MetaData{Links: 2}}
	req3 := Request{Msg: "msg3", UserID: "1", Meta: MetaData{Links: 3}}

	h.Push(req1)
	h.Push(req2)
	h.Push(req3)

	res := h.Last(3)
	require.Equal(t, 3, len(res))
	assert.Equal(t, req1, res[0])
	assert.Equal(t, req2, res[1])
	assert.Equal(t, req3, res[2])
}

func TestLastRequestsOverflow(t *testing.T) {
	h := NewLastRequests(2)
	req1 := Request{Msg: "msg1", UserID: "1"}
	req2 := Request{Msg: "msg2", UserID: "2"}
	req3 := Request{Msg: "msg3", UserID: "1"}

	h.Push(req1)
	h.Push(req2)
	h.Push(req3)

	res := h.Last(3)
	require.Equal(t, 2, len(res))
	assert.Equal(t, req2, res[0])
	assert.Equal(t, req3, res[1])
}

func TestLastRequestsEmpty(t *testing.T) {
	h := NewLastRequests(5)
	assert.Empty(t, h.Last(1))
}

func TestLastRequestsSmallerRequest(t *testing.T) {
	h := NewLastRequests(5)
	req1 := Request{Msg: "msg1", UserID: "1"}
	req2 := Request{Msg: "msg2", UserID: "2"}

	h.Push(req1)
	h.Push(req2)

	res := h.Last(1)
	require.Equal(t, 1, len(res))
	assert.Equal(t, req1, res[0])
}

func TestLastRequestsConcurrent(t *testing.T) {
	h := NewLastRequests(5)
	done := make(chan bool)

	go func() {
		for i := 0; i < 100; i++ {
			h.Push(Request{Msg: "msg", UserID: "1"})
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			h.Last(5)
		}
		done <- true
	}()

	<-done
	<-done
}

func TestLastRequestsNegativeCount(t *testing.T) {
	h := NewLastRequests(5)
	req := Request{Msg: "msg1", UserID: "1"}
	h.Push(req)
	res := h.Last(-1)
	assert.Empty(t, res)
}

func TestLastRequestsZeroSize(t *testing.T) {
	h := NewLastRequests(0)
	assert.Equal(t, 1, h.Size(), "zero size should be clamped to 1")
	req := Request{Msg: "msg1", UserID: "1"}
	h.Push(req)
	res := h.Last(1)
	require.Equal(t, 1, len(res))
	assert.Equal(t, req, res[0])
}

func TestLastRequestsNilValues(t *testing.T) {
	h := NewLastRequests(3)
	h.requests.Value = nil // force nil value
	h.requests = h.requests.Next()
	req := Request{Msg: "msg1", UserID: "1"}
	h.Push(req)
	res := h.Last(2)
	require.Equal(t, 1, len(res))
	assert.Equal(t, req, res[0])
}

func TestLastRequestsVeryLong(t *testing.T) {
	history := NewLastRequests(1)

	// create a very long message
	longMessage := strings.Repeat("a", 2000)

	req := Request{
		Msg:      longMessage,
		UserID:   "user5",
		UserName: "Eve",
	}

	history.Push(req)

	// retrieve and verify
	lastMsgs := history.Last(1)
	require.Len(t, lastMsgs, 1)
	assert.Len(t, lastMsgs[0].Msg, 1024, "Message should be truncated to max length")
}
