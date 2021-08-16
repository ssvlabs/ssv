package threadsafe

import "sync"

var (
	// Bytes returns a new SafeBytes instance
	Bytes = NewSafeBytes
	// BytesS returns a new SafeBytes instance (from string)
	BytesS = NewSafeBytesFromString
)

// SafeBytes is a thread safe []byte
type SafeBytes struct {
	value []byte
	l     sync.RWMutex
}

// NewSafeBytesFromString returns a new SafeBytes from string
func NewSafeBytesFromString(s string) *SafeBytes {
	return NewSafeBytes([]byte(s))
}

// NewSafeBytes returns a new SafeBytes from []byte
func NewSafeBytes(value []byte) *SafeBytes {
	return &SafeBytes{
		value: value,
		l:     sync.RWMutex{},
	}
}

// Get returns thread safe []bytes
func (s *SafeBytes) Get() []byte {
	s.l.RLock()
	defer s.l.RUnlock()
	return s.value
}

// Set sets []byte, thread safe
func (s *SafeBytes) Set(value []byte) {
	s.l.Lock()
	defer s.l.Unlock()
	s.value = value
}
