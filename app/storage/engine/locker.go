package engine

import "sync"

// RWLocker is a read-write locker interface
type RWLocker interface {
	sync.Locker
	RLock()
	RUnlock()
}

// NoopLocker is a no-op locker
type NoopLocker struct{}

// Lock is a no-op
func (NoopLocker) Lock() {}

// Unlock is a no-op
func (NoopLocker) Unlock() {}

// RLock is a no-op
func (NoopLocker) RLock() {}

// RUnlock is a no-op
func (NoopLocker) RUnlock() {}
