package threadsafe

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSafeInt32 tests the SafeInt32 type for thread safety.
func TestSafeInt32(t *testing.T) {
	t.Parallel()

	t.Run("new instance", func(t *testing.T) {
		t.Parallel()

		si := NewSafeInt32(0)

		assert.NotNil(t, si)
		assert.Equal(t, int32(0), si.Get())
	})

	t.Run("set and get", func(t *testing.T) {
		t.Parallel()

		si := NewSafeInt32(0)
		si.Set(42)

		assert.Equal(t, int32(42), si.Get())
	})

	t.Run("concurrent access", func(t *testing.T) {
		t.Parallel()

		si := NewSafeInt32(0)
		var wg sync.WaitGroup
		const numGoroutines = 100

		// test concurrent writes
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				si.Set(42)
			}()
		}
		wg.Wait()

		assert.Equal(t, int32(42), si.Get())

		// test concurrent reads
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = si.Get()
			}()
		}
		wg.Wait()
	})
}
