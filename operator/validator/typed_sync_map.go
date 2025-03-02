package validator

import "sync"

type TypedSyncMap[K comparable, V any] struct {
	sync.Map
}

func NewTypedSyncMap[K comparable, V any]() *TypedSyncMap[K, V] {
	return &TypedSyncMap[K, V]{}
}

func (m *TypedSyncMap[K, V]) Store(key K, value V) {
	m.Map.Store(key, value)
}

func (m *TypedSyncMap[K, V]) Load(key K) (value V, ok bool) {
	v, ok := m.Map.Load(key)
	if ok {
		value = v.(V)
	}

	return
}

func (m *TypedSyncMap[K, V]) Delete(key K) {
	m.Map.Delete(key)
}

func (m *TypedSyncMap[K, V]) Range(f func(key K, value V) bool) {
	m.Map.Range(func(key, value any) bool {
		return f(key.(K), value.(V))
	})
}

func (m *TypedSyncMap[K, V]) Has(key K) bool {
	_, ok := m.Map.Load(key)
	return ok
}

func (m *TypedSyncMap[K, V]) LoadOrStore(key K, value V) (V, bool) {
	v, loaded := m.Map.LoadOrStore(key, value)
	if loaded {
		return v.(V), true
	}

	return value, false
}
