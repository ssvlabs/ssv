package emitter

import (
	"github.com/stretchr/testify/require"
	"sync"
	"sync/atomic"
	"testing"
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

	wg.Add(1)
	go func() {
		cn, close := e.Channel("inc")
		defer wg.Done()
		for data := range cn {
			parsed, ok := data.(testData)
			require.True(t, ok)
			require.True(t, parsed.i > 0)
			close()
		}
	}()

	_ = e.On("inc", func(data EventData) {
		if toAdd, ok := data.(testData); ok {
			atomic.AddInt64(&i, toAdd.i)
		}
		if atomic.LoadInt64(&i) >= int64(20) {
			wg.Done()
		}
	})
	deregister := e.On("inc", func(data EventData) {
		if toAdd, ok := data.(testData); ok {
			atomic.AddInt64(&i, toAdd.i)
		}
	})

	wg.Add(1)
	go func() {
		e.Notify("inc", testData{i: int64(5)})
		e.Notify("inc", testData{i: int64(3)})
		deregister()
		e.Notify("inc", testData{i: int64(2)})
		e.Notify("inc", testData{i: int64(2)})
	}()

	wg.Wait()
	require.Equal(t, int64(20), atomic.LoadInt64(&i))
	e.(*emitter).mut.Lock()
	require.Equal(t, 1, len(e.(*emitter).handlers["inc"]))
	e.(*emitter).mut.Unlock()
}
