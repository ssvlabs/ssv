package ttl

import (
	"context"
	"time"

	"github.com/ssvlabs/ssv/utils/hashmap"
)

// Map implements a thread-safe map with automatic TTL-based expiry.
type Map[Key comparable, Value any] struct {
	*hashmap.Map[Key, Value]
	idxLastUpdatedAt hashmap.Map[Key, time.Time]
}

// New creates a TTL map that automatically removes entries older than lifespan.
// The cleanup goroutine runs every cleanupInterval and exits when ctx is canceled.
// After ctx is canceled, the map remains readable but should not be used for new writes —
// cleanup is permanently stopped and timestamp metadata from new writes will not be reclaimed.
func New[Key comparable, Value any](ctx context.Context, lifespan, cleanupInterval time.Duration) *Map[Key, Value] {
	m := &Map[Key, Value]{
		Map:              hashmap.New[Key, Value](),
		idxLastUpdatedAt: hashmap.Map[Key, time.Time]{},
	}
	go func() {
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.idxLastUpdatedAt.Range(func(key Key, t time.Time) bool {
					if time.Since(t) > lifespan {
						m.Delete(key)
					}
					return true
				})
			}
		}
	}()
	return m
}

func (m *Map[Key, Value]) GetOrSet(key Key, value Value) (Value, bool) {
	result, loaded := m.Map.GetOrSet(key, value)
	if !loaded {
		m.idxLastUpdatedAt.Set(key, time.Now())
	}
	return result, loaded
}

func (m *Map[Key, Value]) CompareAndSwap(key Key, oldValue, newValue Value) (swapped bool) {
	swapped = m.Map.CompareAndSwap(key, oldValue, newValue)
	if swapped {
		m.idxLastUpdatedAt.Set(key, time.Now())
	}
	return swapped
}

func (m *Map[Key, Value]) Delete(key Key) bool {
	m.idxLastUpdatedAt.Delete(key)
	return m.Map.Delete(key)
}

func (m *Map[Key, Value]) GetAndDelete(key Key) (Value, bool) {
	m.idxLastUpdatedAt.Delete(key)
	return m.Map.GetAndDelete(key)
}

func (m *Map[Key, Value]) Set(key Key, value Value) {
	m.Map.Set(key, value)
	m.idxLastUpdatedAt.Set(key, time.Now())
}
