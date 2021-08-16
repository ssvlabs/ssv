package threadsafe

import "sync"

// SafeInt64 is a thread-safe wrapper on top of int64
type SafeInt64 struct {
	value int64
	l     sync.RWMutex
}

// NewSafeInt64 creates a new instance
func NewSafeInt64(value int64) *SafeInt64 {
	return &SafeInt64{
		value: value,
		l:     sync.RWMutex{},
	}
}

// Get returns the underlying value
func (s *SafeInt64) Get() int64 {
	s.l.RLock()
	defer s.l.RUnlock()
	return s.value
}

// Set enables to change value
func (s *SafeInt64) Set(value int64) {
	s.l.Lock()
	defer s.l.Unlock()
	s.value = value
}
