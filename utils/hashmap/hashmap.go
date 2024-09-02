package hashmap

import (
	"sync"
)

type Map[Key comparable, Value any] struct {
	m  map[Key]Value
	mu sync.RWMutex
}

func New[Key comparable, Value any]() *Map[Key, Value] {
	return &Map[Key, Value]{
		m:  make(map[Key]Value),
		mu: sync.RWMutex{},
	}
}

func (m *Map[Key, Value]) Get(key Key) (Value, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	v, ok := m.m[key]
	return v, ok
}

func (m *Map[Key, Value]) GetOrInsert(key Key, value Value) (Value, bool) {
	m.mu.RLock()
	v, ok := m.m[key]
	m.mu.RUnlock()

	if !ok {
		m.mu.Lock()
		defer m.mu.Unlock()

		m.m[key] = value
		v = value
	}
	return v, ok
}

func (m *Map[Key, Value]) Set(key Key, value Value) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.m[key] = value
}

func (m *Map[Key, Value]) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.m)
}

func (m *Map[Key, Value]) Range(f func(Key, Value) bool) {
	for k, v := range m.m {
		if !f(k, v) {
			break
		}
	}
}

func (m *Map[Key, Value]) Del(key Key) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, found := m.m[key]
	if found {
		delete(m.m, key)
	}
	return found
}
