package pubsub

import (
	"github.com/stretchr/testify/require"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testData struct {
	i int64
}

func (td testData) Copy() interface{} {
	return testData{td.i}
}

func TestNewEmitter(t *testing.T) {
	var i int64
	e := NewEmitter()
	var wg sync.WaitGroup

	// checks that channel works
	cn, deregister := e.Channel("inc")
	wg.Add(1)
	go func() {
		defer wg.Done()
		for data := range cn {
			parsed, ok := data.(testData)
			require.True(t, ok)
			atomic.AddInt64(&i, parsed.i)
		}
	}()

	deregisterOn := e.On("inc", func(data EventData) {
		if toAdd, ok := data.(testData); ok {
			atomic.AddInt64(&i, toAdd.i)
		}
		if atomic.LoadInt64(&i) >= int64(20) {
			wg.Done()
		}
	})
	defer deregisterOn()

	wg.Add(1)
	go func() {
		e.Notify("inc", testData{i: int64(5)})
		e.Notify("inc", testData{i: int64(3)})
		// sleep before calling deregister as Notify works with goroutines
		time.Sleep(10 * time.Millisecond)
		deregister()
		go e.Notify("inc", testData{i: int64(2)})
		go e.Notify("inc", testData{i: int64(2)})
	}()

	wg.Wait()
	require.Equal(t, int64(20), atomic.LoadInt64(&i))
	require.Equal(t, 1, e.(*emitter).countHandlers("inc"))
}

func TestEmitter_Once(t *testing.T) {
	var i int64
	e := NewEmitter()

	e.Once("inc", func(data EventData) {
		if toAdd, ok := data.(testData); ok {
			atomic.AddInt64(&i, -toAdd.i)
		}
	})
	e.On("inc", func(data EventData) {
		if toAdd, ok := data.(testData); ok {
			atomic.AddInt64(&i, toAdd.i)
		}
	})

	e.Notify("inc", testData{i: int64(5)})
	e.Notify("inc", testData{i: int64(5)})

	time.Sleep(10 * time.Millisecond)

	require.Equal(t, int64(5), atomic.LoadInt64(&i))

	e.(*emitter).mut.Lock()
	require.Equal(t, 1, len(e.(*emitter).handlers["inc"]))
	e.(*emitter).mut.Unlock()
}
