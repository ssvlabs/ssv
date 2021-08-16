package threadsafe

import "sync"

var (
	Int32 = NewSafeInt32
)

type SafeInt32 struct {
	value int32
	l     sync.RWMutex
}

func NewSafeInt32(value int32) *SafeInt32 {
	return &SafeInt32{
		value: value,
		l:     sync.RWMutex{},
	}
}

func (s *SafeInt32) Get() int32 {
	s.l.RLock()
	defer s.l.RUnlock()
	return s.value
}

func (s *SafeInt32) Set(value int32) {
	s.l.Lock()
	defer s.l.Unlock()
	s.value = value
}
