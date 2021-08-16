package threadsafe

import "sync"

// SafeInt64 is a thread safe int64 equivalent
type SafeInt64 struct {
	value int64
	l     sync.RWMutex
}

// NewSafeInt64 returns a new SafeInt64 instance
func NewSafeInt64(value int64) *SafeInt64 {
	return &SafeInt64{
		value: value,
		l:     sync.RWMutex{},
	}
}

// Get returns thread safe int64
func (s *SafeInt64) Get() int64 {
	s.l.RLock()
	defer s.l.RUnlock()
	return s.value
}

// Set sets int64, thread safe
func (s *SafeInt64) Set(value int64) {
	s.l.Lock()
	defer s.l.Unlock()
	s.value = value
}
