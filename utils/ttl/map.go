package ttl

import (
	"time"

	"github.com/ssvlabs/ssv/utils/hashmap"
)

// Map implements a thread-safe map with a sync.Map under the hood.
type Map[Key comparable, Value any] struct {
	*hashmap.Map[Key, Value]
	idxLastUpdatedAt hashmap.Map[Key, time.Time]
}

func New[Key comparable, Value any](lifespan, cleanupInterval time.Duration) *Map[Key, Value] {
	m := &Map[Key, Value]{
		Map:              hashmap.New[Key, Value](),
		idxLastUpdatedAt: hashmap.Map[Key, time.Time]{},
	}
	go func() {
		// TODO: use time.After when Go is updated to 1.23
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()

		// TODO - consider terminating with ctx.Done() to make this ttl map garbage-collectable
		for range ticker.C {
			m.idxLastUpdatedAt.Range(func(key Key, t time.Time) bool {
				if time.Since(t) > lifespan {
					m.idxLastUpdatedAt.Delete(key)
					m.Delete(key)
				}
				return true
			})
		}
	}()
	return m
}

func (m *Map[Key, Value]) GetOrSet(key Key, value Value) (Value, bool) {
	result, loaded := m.Map.GetOrSet(key, value)
	if !loaded {
		// gotta update timestamp sine we've just set value for this key
		m.idxLastUpdatedAt.Set(key, time.Now())
	}
	return result, loaded
}

func (m *Map[Key, Value]) CompareAndSwap(key Key, old, new Value) (swapped bool) {
	swapped = m.Map.CompareAndSwap(key, old, new)
	if swapped {
		// gotta update timestamp sine we've just set value for this key
		m.idxLastUpdatedAt.Set(key, time.Now())
	}
	return swapped
}

func (m *Map[Key, Value]) Set(key Key, value Value) {
	m.Map.Set(key, value)
	m.idxLastUpdatedAt.Set(key, time.Now())
}
