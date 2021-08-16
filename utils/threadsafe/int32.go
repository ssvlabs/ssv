package threadsafe

import "sync"

var (
	// Int32 returns a new SafeInt32 instance
	Int32 = NewSafeInt32
)

// SafeInt32 is a thread safe int32 equivalent
type SafeInt32 struct {
	value int32
	l     sync.RWMutex
}

// NewSafeInt32 returns a new SafeInt32 instance
func NewSafeInt32(value int32) *SafeInt32 {
	return &SafeInt32{
		value: value,
		l:     sync.RWMutex{},
	}
}

// Get returns thread safe int32
func (s *SafeInt32) Get() int32 {
	s.l.RLock()
	defer s.l.RUnlock()
	return s.value
}

// Set sets int32, thread safe
func (s *SafeInt32) Set(value int32) {
	s.l.Lock()
	defer s.l.Unlock()
	s.value = value
}
