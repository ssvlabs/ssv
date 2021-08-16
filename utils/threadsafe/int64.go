package threadsafe

import "sync"

type SafeInt64 struct {
	value int64
	l     sync.RWMutex
}

func NewSafeInt64(value int64) *SafeInt64 {
	return &SafeInt64{
		value: value,
		l:     sync.RWMutex{},
	}
}

func (s *SafeInt64) Get() int64 {
	s.l.RLock()
	defer s.l.RUnlock()
	return s.value
}

func (s *SafeInt64) Set(value int64) {
	s.l.Lock()
	defer s.l.Unlock()
	s.value = value
}
