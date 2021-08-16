package threadsafe

import "sync"

var (
	// Uint64 returns a new SafeUint64 instance
	Uint64 = NewSafeUint64
)

// SafeUint64 is a thread safe uint64 equivalent
type SafeUint64 struct {
	value uint64
	l     sync.RWMutex
}

// NewSafeUint64 returns a new SafeUint64 instance
func NewSafeUint64(value uint64) *SafeUint64 {
	return &SafeUint64{
		value: value,
		l:     sync.RWMutex{},
	}
}

// Get returns thread safe uint64
func (s *SafeUint64) Get() uint64 {
	s.l.RLock()
	defer s.l.RUnlock()
	return s.value
}

// Set sets uint64, thread safe
func (s *SafeUint64) Set(value uint64) {
	s.l.Lock()
	defer s.l.Unlock()
	s.value = value
}
