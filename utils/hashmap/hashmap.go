package hashmap

import (
	"fmt"
	"strings"
	"sync"
)

// Map implements a thread-safe map with a sync.Map under the hood.
type Map[Key comparable, Value any] struct {
	m sync.Map
}

func New[Key comparable, Value any]() *Map[Key, Value] {
	return &Map[Key, Value]{}
}

func (m *Map[Key, Value]) Get(key Key) (Value, bool) {
	v, ok := m.m.Load(key)
	if !ok {
		var zero Value
		return zero, false
	}
	return v.(Value), true
}

func (m *Map[Key, Value]) GetOrSet(key Key, value Value) (Value, bool) {
	actual, loaded := m.m.LoadOrStore(key, value)
	return actual.(Value), loaded
}

func (m *Map[Key, Value]) Set(key Key, value Value) {
	m.m.Store(key, value)
}

func (m *Map[Key, Value]) Len() int {
	length := 0
	m.m.Range(func(_, _ any) bool {
		length++
		return true
	})
	return length
}

func (m *Map[Key, Value]) Range(f func(Key, Value) bool) {
	m.m.Range(func(k, v any) bool {
		return f(k.(Key), v.(Value))
	})
}

func (m *Map[Key, Value]) Delete(key Key) bool {
	_, found := m.m.Load(key)
	if found {
		m.m.Delete(key)
	}
	return found
}

func (m *Map[Key, Value]) String() string {
	var b strings.Builder
	i := 0
	b.WriteString("[")
	m.m.Range(func(k, v any) bool {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(fmt.Sprintf("%v=%v", k, v))
		i++
		return true
	})
	b.WriteString("]")
	return b.String()
}
