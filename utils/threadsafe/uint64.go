package threadsafe

import "sync"

var (
	Uint64 = NewSafeUint64
)

type SafeUint64 struct {
	value uint64
	l     sync.RWMutex
}

func NewSafeUint64(value uint64) *SafeUint64 {
	return &SafeUint64{
		value: value,
		l:     sync.RWMutex{},
	}
}

func (s *SafeUint64) Get() uint64 {
	s.l.RLock()
	defer s.l.RUnlock()
	return s.value
}

func (s *SafeUint64) Set(value uint64) {
	s.l.Lock()
	defer s.l.Unlock()
	s.value = value
}
