package threadsafe

import "sync"

var (
	// Uint64 is a shorthand for NewSafeUint64
	Uint64 = NewSafeUint64
)

// SafeUint64 is a thread-safe wrapper on top of uint64
type SafeUint64 struct {
	value uint64
	l     sync.RWMutex
}

// NewSafeUint64 creates a new instance
func NewSafeUint64(value uint64) *SafeUint64 {
	return &SafeUint64{
		value: value,
		l:     sync.RWMutex{},
	}
}

// Get returns the underlying value
func (s *SafeUint64) Get() uint64 {
	s.l.RLock()
	defer s.l.RUnlock()
	return s.value
}

// Set enables to change value
func (s *SafeUint64) Set(value uint64) {
	s.l.Lock()
	defer s.l.Unlock()
	s.value = value
}
