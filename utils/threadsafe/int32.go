package threadsafe

import "sync"

var (
	// Int32 is a shorthand for NewSafeInt32
	Int32 = NewSafeInt32
)

// SafeInt32 is a thread-safe wrapper on top of int32
type SafeInt32 struct {
	value int32
	l     sync.RWMutex
}

// NewSafeInt32 creates a new instance
func NewSafeInt32(value int32) *SafeInt32 {
	return &SafeInt32{
		value: value,
		l:     sync.RWMutex{},
	}
}

// Get returns the underlying value
func (s *SafeInt32) Get() int32 {
	s.l.RLock()
	defer s.l.RUnlock()
	return s.value
}

// Set enables to change value
func (s *SafeInt32) Set(value int32) {
	s.l.Lock()
	defer s.l.Unlock()
	s.value = value
}
