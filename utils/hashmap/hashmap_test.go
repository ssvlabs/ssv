package hashmap

import (
	"encoding/hex"
	"fmt"
	"math/rand"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Credit: most of the tests here are borrowed from the hashmap package.
// https://github.com/cornelk/hashmap/blob/28d6fb92c67132d1bf08a5e07c81fdc5855bb460/hashmap_test.go

func TestNew(t *testing.T) {
	t.Parallel()
	m := New[uintptr, uintptr]()
	assert.Equal(t, 0, m.Len())
}

func TestSetString(t *testing.T) {
	t.Parallel()
	m := New[int, string]()
	elephant := "elephant"
	monkey := "monkey"

	m.Set(1, elephant) // insert
	value, ok := m.Get(1)
	assert.True(t, ok)
	assert.Equal(t, elephant, value)

	m.Set(1, monkey) // overwrite
	value, ok = m.Get(1)
	assert.True(t, ok)
	assert.Equal(t, monkey, value)

	assert.Equal(t, 1, m.Len())

	m.Set(2, elephant) // insert
	assert.Equal(t, 2, m.Len())
	value, ok = m.Get(2)
	assert.True(t, ok)
	assert.Equal(t, elephant, value)
}

func TestSetUint8(t *testing.T) {
	t.Parallel()
	m := New[uint8, int]()

	m.Set(1, 128) // insert
	value, ok := m.Get(1)
	assert.True(t, ok)
	assert.Equal(t, 128, value)

	m.Set(2, 200) // insert
	assert.Equal(t, 2, m.Len())
	value, ok = m.Get(2)
	assert.True(t, ok)
	assert.Equal(t, 200, value)
}

func TestSetInt16(t *testing.T) {
	t.Parallel()
	m := New[int16, int]()

	m.Set(1, 128) // insert
	value, ok := m.Get(1)
	assert.True(t, ok)
	assert.Equal(t, 128, value)

	m.Set(2, 200) // insert
	assert.Equal(t, 2, m.Len())
	value, ok = m.Get(2)
	assert.True(t, ok)
	assert.Equal(t, 200, value)
}

func TestSetFloat32(t *testing.T) {
	t.Parallel()
	m := New[float32, int]()

	m.Set(1.1, 128) // insert
	value, ok := m.Get(1.1)
	assert.True(t, ok)
	assert.Equal(t, 128, value)

	m.Set(2.2, 200) // insert
	assert.Equal(t, 2, m.Len())
	value, ok = m.Get(2.2)
	assert.True(t, ok)
	assert.Equal(t, 200, value)
}

func TestSetFloat64(t *testing.T) {
	t.Parallel()
	m := New[float64, int]()

	m.Set(1.1, 128) // insert
	value, ok := m.Get(1.1)
	assert.True(t, ok)
	assert.Equal(t, 128, value)

	m.Set(2.2, 200) // insert
	assert.Equal(t, 2, m.Len())
	value, ok = m.Get(2.2)
	assert.True(t, ok)
	assert.Equal(t, 200, value)
}

func TestSetInt64(t *testing.T) {
	t.Parallel()
	m := New[int64, int]()

	m.Set(1, 128) // insert
	value, ok := m.Get(1)
	assert.True(t, ok)
	assert.Equal(t, 128, value)

	m.Set(2, 200) // insert
	assert.Equal(t, 2, m.Len())
	value, ok = m.Get(2)
	assert.True(t, ok)
	assert.Equal(t, 200, value)
}

func TestByteArray(t *testing.T) {
	t.Parallel()
	m := New[[4]byte, int]()

	m.Set([4]byte{1, 2, 3, 4}, 128) // insert
	value, ok := m.Get([4]byte{1, 2, 3, 4})
	assert.True(t, ok)
	assert.Equal(t, 128, value)

	m.Delete([4]byte{1, 2, 3, 4})
	assert.Equal(t, 0, m.Len())
	_, ok = m.Get([4]byte{1, 2, 3, 4})
	assert.False(t, ok)
	assert.Equal(t, 0, m.Len())
}

func TestGetNonExistingItem(t *testing.T) {
	t.Parallel()
	m := New[int, string]()
	value, ok := m.Get(1)
	assert.False(t, ok)
	assert.Equal(t, "", value)
}

func TestStringer(t *testing.T) {
	t.Parallel()
	m := New[int, string]()

	// Test with zero items
	assert.Equal(t, "[]", m.String())

	// Test with one item
	m.Set(1, "elephant")
	assert.Equal(t, "[1=elephant]", m.String())

	// Test with two items
	m.Set(2, "monkey")
	expectedStrings := []string{"[1=elephant, 2=monkey]", "[2=monkey, 1=elephant]"}
	actualString := m.String()
	assert.Contains(t, expectedStrings, actualString)
}

func TestDelete(t *testing.T) {
	t.Parallel()
	m := New[int, string]()
	elephant := "elephant"
	monkey := "monkey"

	deleted := m.Delete(1)
	assert.False(t, deleted)

	m.Set(1, elephant)
	m.Set(2, monkey)

	deleted = m.Delete(0)
	assert.False(t, deleted)
	deleted = m.Delete(3)
	assert.False(t, deleted)
	assert.Equal(t, 2, m.Len())

	deleted = m.Delete(1)
	assert.True(t, deleted)
	deleted = m.Delete(1)
	assert.False(t, deleted)
	deleted = m.Delete(2)
	assert.True(t, deleted)
	assert.Equal(t, 0, m.Len())
}

func TestRange(t *testing.T) {
	t.Parallel()
	m := New[int, string]()

	items := map[int]string{}
	m.Range(func(key int, value string) bool {
		items[key] = value
		return true
	})
	assert.Equal(t, 0, len(items))

	itemCount := 16
	for i := itemCount; i > 0; i-- {
		m.Set(i, strconv.Itoa(i))
	}

	items = map[int]string{}
	m.Range(func(key int, value string) bool {
		items[key] = value
		return true
	})

	assert.Equal(t, itemCount, len(items))
	for i := 1; i <= itemCount; i++ {
		value, ok := items[i]
		assert.True(t, ok)
		expected := strconv.Itoa(i)
		assert.Equal(t, expected, value)
	}

	items = map[int]string{} // test aborting range
	m.Range(func(key int, value string) bool {
		items[key] = value
		return false
	})
	assert.Equal(t, 1, len(items))
}

// nolint: funlen, gocognit
func TestHashMap_parallel(t *testing.T) {
	m := New[int, int]()

	max := 10
	dur := 2 * time.Second

	do := func(t *testing.T, max int, d time.Duration, fn func(*testing.T, int)) <-chan error {
		t.Helper()
		done := make(chan error)
		var times int64
		// This goroutines will terminate test in case if closure hangs.
		go func() {
			for {
				select {
				case <-time.After(d + 500*time.Millisecond):
					if atomic.LoadInt64(&times) == 0 {
						done <- fmt.Errorf("closure was not executed even once, something blocks it")
					}
					close(done)
				case <-done:
				}
			}
		}()
		go func() {
			timer := time.NewTimer(d)
			defer timer.Stop()
		InfLoop:
			for {
				for i := 0; i < max; i++ {
					select {
					case <-timer.C:
						break InfLoop
					default:
					}
					fn(t, i)
					atomic.AddInt64(&times, 1)
				}
			}
			close(done)
		}()
		return done
	}

	wait := func(t *testing.T, done <-chan error) {
		t.Helper()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}

	// Initial fill.
	for i := 0; i < max; i++ {
		m.Set(i, i)
	}
	t.Run("set_get", func(t *testing.T) {
		doneSet := do(t, max, dur, func(t *testing.T, i int) {
			t.Helper()
			m.Set(i, i)
		})
		doneGet := do(t, max, dur, func(t *testing.T, i int) {
			t.Helper()
			if _, ok := m.Get(i); !ok {
				t.Errorf("missing value for key: %d", i)
			}
		})
		wait(t, doneSet)
		wait(t, doneGet)
	})
	t.Run("get-or-insert-and-delete", func(t *testing.T) {
		doneGetOrInsert := do(t, max, dur, func(t *testing.T, i int) {
			t.Helper()
			m.GetOrSet(i, i)
		})
		doneDel := do(t, max, dur, func(t *testing.T, i int) {
			t.Helper()
			m.Delete(i)
		})
		wait(t, doneGetOrInsert)
		wait(t, doneDel)
	})
}

func TestHashMap_SetConcurrent(t *testing.T) {
	t.Parallel()
	m := New[string, int]()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			m.Set(strconv.Itoa(i), i)

			wg.Add(1)
			go func(i int) {
				defer wg.Done()

				m.Get(strconv.Itoa(i))
			}(i)
		}(i)
	}

	wg.Wait()
}

func TestConcurrentInsertDelete(t *testing.T) {
	t.Parallel()

	for i := 0; i < 200; i++ {
		l := New[int, int]()
		l.Set(111, 111)
		l.Set(222, 222)
		l.Set(333, 333)
		wg := sync.WaitGroup{}
		wg.Add(2)

		go func() {
			time.Sleep(time.Duration(randSeed().Intn(10)))
			l.Delete(222)
			wg.Done()
		}()
		go func() {
			defer wg.Done()
			time.Sleep(time.Duration(randSeed().Intn(10)))
			l.GetOrSet(223, 223)
		}()
		wg.Wait()

		assert.Equal(t, 3, l.Len())
		_, found := l.Get(223)
		assert.True(t, found)
	}
}

func TestGetOrInsert(t *testing.T) {
	t.Parallel()
	m := New[int, string]()
	value, ok := m.GetOrSet(1, "1")
	assert.False(t, ok)
	assert.Equal(t, "1", value)

	value, ok = m.GetOrSet(1, "2")
	assert.True(t, ok)
	assert.Equal(t, "1", value)
}

func TestGetOrInsertHangIssue67(_ *testing.T) {
	m := New[string, int]()

	var wg sync.WaitGroup
	key := "key"

	wg.Add(1)
	go func() {
		defer wg.Done()
		m.GetOrSet(key, 9)
		m.Delete(key)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		m.GetOrSet(key, 9)
		m.Delete(key)
	}()

	wg.Wait()
}

// See https://github.com/ssvlabs/ssv/issues/1682
func TestIssue1682(t *testing.T) {
	type validatorStatus int

	const (
		validatorStatusSubscribing validatorStatus = 1
		validatorStatusSubscribed  validatorStatus = 2
	)

	cmtIDsHex := []string{
		"9576fdcd4cfd9e563a5ce54e1a2e8a2950a94d8d0db49696f37889929ad813fd",
		"b9492d60036f93841da23f7ee49f987ace6cb7d07170b9e9aaa2be54f4fceaf7",
		"e2394daed1f3bfc1714dea43d0aaf663a10af88ed985aed5564e3d027d961b89",
	}
	var cmtIDs []string
	for _, cmtIDHex := range cmtIDsHex {
		cmtID, err := hex.DecodeString(cmtIDHex)
		require.NoError(t, err)
		cmtIDs = append(cmtIDs, string(cmtID))
	}

	var wwg sync.WaitGroup
	var errs []error
	var mu sync.Mutex
	for i := 0; i < 10; i++ {
		wwg.Add(1)
		go func() {
			defer wwg.Done()

			m := New[string, validatorStatus]()
			var wg sync.WaitGroup
			for _, cmtID := range cmtIDs {
				cmtID := cmtID
				n := 50 + randSeed().Intn(200)
				for j := 0; j < n; j++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						time.Sleep(time.Duration(randSeed().Intn(2000)) * time.Millisecond)
						_, found := m.GetOrSet(cmtID, validatorStatusSubscribing)
						if !found {
							time.Sleep(time.Duration(randSeed().Intn(200)) * time.Millisecond)
							m.Set(cmtID, validatorStatusSubscribed)
						} else {
							time.Sleep(time.Duration(randSeed().Intn(200)) * time.Millisecond)
						}
					}()
				}
			}

			done := make(chan struct{})
			go func() {
				defer close(done)
				wg.Wait()
			}()

			ticker := time.NewTicker(1 * time.Millisecond)
			var keys []string
			defer ticker.Stop()
		Loop:
			for {
				keys = []string{}
				m.Range(func(key string, value validatorStatus) bool {
					keys = append(keys, key)
					return true
				})
				select {
				case <-done:
					keys = []string{}
					m.Range(func(key string, value validatorStatus) bool {
						keys = append(keys, key)
						return true
					})
					keysHex := []string{}
					for _, key := range keys {
						keysHex = append(keysHex, hex.EncodeToString([]byte(key)))
					}
					t.Logf("iteration %d: %d keys, %d expected", i, len(keys), len(cmtIDs))
					if len(keys) != len(cmtIDs) {
						t.Logf("expected %d keys, got %d (%v)", len(cmtIDs), len(keys), keysHex)
						mu.Lock()
						errs = append(errs, fmt.Errorf("expected %d keys, got %d (%v)", len(cmtIDs), len(keys), keysHex))
						mu.Unlock()
					}
					break Loop
				case <-ticker.C:
				}
			}
		}()
	}
	wwg.Wait()
	require.Empty(t, errs)
}

func randSeed() *rand.Rand {
	return rand.New(rand.NewSource(time.Now().UnixNano()))
}
