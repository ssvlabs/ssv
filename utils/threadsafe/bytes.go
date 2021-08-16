package threadsafe

import "sync"

type SafeBytes struct {
	value []byte
	l     sync.RWMutex
}

func NewSafeBytes(value []byte) *SafeBytes {
	return &SafeBytes{
		value: value,
		l:     sync.RWMutex{},
	}
}

func (s *SafeBytes) Get() []byte {
	s.l.RLock()
	defer s.l.RUnlock()
	return s.value
}

func (s *SafeBytes) Set(value []byte) {
	s.l.Lock()
	defer s.l.Unlock()
	s.value = value
}
