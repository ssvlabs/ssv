package hashmap

import (
	"fmt"
	"strings"
	"sync"
)

// Map implements a thread-safe map with a sync.Map under the hood, it's best for read-heavy workloads.
type Map[Key comparable, Value any] struct {
	m sync.Map
}

func New[Key comparable, Value any]() *Map[Key, Value] {
	return &Map[Key, Value]{}
}

func (m *Map[Key, Value]) Has(key Key) bool {
	_, ok := m.m.Load(key)
	return ok
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

func (m *Map[Key, Value]) CompareAndSwap(key Key, oldValue, newValue Value) (swapped bool) {
	return m.m.CompareAndSwap(key, oldValue, newValue)
}

func (m *Map[Key, Value]) Set(key Key, value Value) {
	m.m.Store(key, value)
}

// SlowLen returns the number of elements in the map by iterating over all items.
// This method call doesn't block other operations (for example Set), hence the
// resulting length value returned might or might not reflect the outcome of
// concurrent operations happening with this Map.
//
// This implementation is quite expensive. If it becomes a bottleneck,
// we should consider maintaining an internal atomic counter and
// using LoadOrStore and LoadAndDelete exclusively to update it.
//
// With that said, this would hurt the performance of writes and deletes,
// so perhaps it should be a separate implementation (such as MapWithLen).
func (m *Map[Key, Value]) SlowLen() int {
	n := 0
	m.m.Range(func(_, _ any) bool {
		n++
		return true
	})
	return n
}

func (m *Map[Key, Value]) Range(f func(Key, Value) bool) {
	m.m.Range(func(k, v any) bool {
		return f(k.(Key), v.(Value))
	})
}

func (m *Map[Key, Value]) Delete(key Key) bool {
	_, loaded := m.m.LoadAndDelete(key)
	return loaded
}

func (m *Map[Key, Value]) GetAndDelete(key Key) (Value, bool) {
	v, loaded := m.m.LoadAndDelete(key)
	if !loaded {
		var zero Value
		return zero, false
	}
	return v.(Value), true
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
