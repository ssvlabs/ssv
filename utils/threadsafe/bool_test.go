package threadsafe

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSafeBool tests the SafeBool type for thread safety.
func TestSafeBool(t *testing.T) {
	t.Parallel()

	t.Run("new instance", func(t *testing.T) {
		t.Parallel()

		sb := NewSafeBool()

		assert.NotNil(t, sb)
		assert.False(t, sb.Get())
	})

	t.Run("set and get", func(t *testing.T) {
		t.Parallel()

		sb := NewSafeBool()
		sb.Set(true)

		assert.True(t, sb.Get())

		sb.Set(false)

		assert.False(t, sb.Get())
	})

	t.Run("concurrent access", func(t *testing.T) {
		t.Parallel()

		sb := NewSafeBool()
		var wg sync.WaitGroup
		const numGoroutines = 100

		// test concurrent writes
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sb.Set(true)
			}()
		}
		wg.Wait()

		assert.True(t, sb.Get())

		// test concurrent reads
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = sb.Get()
			}()
		}
		wg.Wait()
	})
}
