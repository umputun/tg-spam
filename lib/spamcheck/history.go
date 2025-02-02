package spamcheck

import (
	"container/ring"
	"sync"
)

// LastRequests keeps track of last N requests, thread-safe.
type LastRequests struct {
	requests *ring.Ring
	size     int
	lock     sync.RWMutex
}

const maxMsgLen = 1024

// NewLastRequests creates new requests tracker
func NewLastRequests(size int) *LastRequests {
	// minimum size is 1
	if size < 1 {
		size = 1
	}
	return &LastRequests{
		requests: ring.New(size),
		size:     size,
	}
}

// Push adds new request to the history
func (h *LastRequests) Push(req Request) {
	h.lock.Lock()
	defer h.lock.Unlock()

	if len(req.Msg) > maxMsgLen {
		// truncate the message if it's too long to prevent memory exhaustion attacks
		req.Msg = req.Msg[:maxMsgLen]
	}

	h.requests.Value = req
	h.requests = h.requests.Next()
}

// Last returns up to n last requests in chronological order (oldest to newest)
func (h *LastRequests) Last(n int) []Request {
	if n < 1 {
		return []Request{}
	}

	h.lock.RLock()
	defer h.lock.RUnlock()

	if n > h.size {
		n = h.size
	}

	result := make([]Request, 0, n)
	h.requests.Do(func(v interface{}) {
		if v != nil {
			if req, ok := v.(Request); ok {
				result = append(result, req)
			}
		}
	})

	if len(result) > n {
		result = result[:n]
	}
	return result
}

// Size returns the size of request history
func (h *LastRequests) Size() int {
	return h.size
}
