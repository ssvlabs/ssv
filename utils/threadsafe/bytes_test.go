package threadsafe

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSafeBytes tests the SafeBytes type for thread safety.
func TestSafeBytes(t *testing.T) {
	t.Parallel()

	t.Run("new instance", func(t *testing.T) {
		t.Parallel()

		sb := NewSafeBytes([]byte{})

		assert.NotNil(t, sb)
		assert.Empty(t, sb.Get())
	})

	t.Run("new instance from string", func(t *testing.T) {
		t.Parallel()

		testData := "test data"
		sb := NewSafeBytesFromString(testData)

		assert.Equal(t, []byte(testData), sb.Get())
	})

	t.Run("set and get", func(t *testing.T) {
		t.Parallel()

		sb := NewSafeBytes([]byte{})
		testData := []byte("test data")
		sb.Set(testData)

		assert.Equal(t, testData, sb.Get())
	})

	t.Run("concurrent access", func(t *testing.T) {
		t.Parallel()

		sb := NewSafeBytes([]byte{})
		var wg sync.WaitGroup
		const numGoroutines = 100

		// test concurrent writes
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sb.Set([]byte("test"))
			}()
		}
		wg.Wait()

		assert.Equal(t, []byte("test"), sb.Get())

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
