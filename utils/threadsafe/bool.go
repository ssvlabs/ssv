package threadsafe

import "sync"

var (
	// Bytes returns a new SafeBytes instance
	Bool = NewSafeBool
)

// SafeBool is a thread safe []byte
type SafeBool struct {
	value bool
	l     sync.RWMutex
}

// NewSafeBool returns a new SafeBytes from []byte
func NewSafeBool() *SafeBool {
	return &SafeBool{
		l: sync.RWMutex{},
	}
}

// Get returns thread safe []bytes
func (s *SafeBool) Get() bool {
	s.l.RLock()
	defer s.l.RUnlock()
	return s.value
}

// Set sets []byte, thread safe
func (s *SafeBool) Set(value bool) {
	s.l.Lock()
	defer s.l.Unlock()
	s.value = value
}
