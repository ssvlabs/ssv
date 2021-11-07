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
		parsed, ok := data.(testData)
		require.True(t, ok)
		atomic.AddInt64(&i, parsed.i)
		if atomic.LoadInt64(&i) >= int64(20) {
			wg.Done()
		}
	})
	defer deregisterOn()

	require.Equal(t, 2, e.(*emitter).countHandlers("inc"))

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
		parsed, ok := data.(testData)
		require.True(t, ok)
		atomic.AddInt64(&i, -parsed.i)
	})
	e.On("inc", func(data EventData) {
		parsed, ok := data.(testData)
		require.True(t, ok)
		atomic.AddInt64(&i, parsed.i)
	})

	e.Notify("inc", testData{i: int64(5)})
	e.Notify("inc", testData{i: int64(5)})

	time.Sleep(10 * time.Millisecond)

	require.Equal(t, int64(5), atomic.LoadInt64(&i))

	require.Equal(t, 1, e.(*emitter).countHandlers("inc"))
}

//func TestEmitter_Stress(t *testing.T) {
//	const n = 10000
//	const nEvents = 100
//	e := NewEmitter()
//	var numbers [n]int64
//
//	register := func(i int) {
//		numbers[i] = 0
//		e.On("inc", func(data EventData) {
//			parsed, ok := data.(testData)
//			require.True(t, ok)
//			atomic.AddInt64(&numbers[i], parsed.i)
//		})
//	}
//	for i := 0; i < n; i++ {
//		register(i)
//	}
//
//	fmt.Println("registered")
//
//	var wg sync.WaitGroup
//	wg.Add(1)
//	go func() {
//		defer wg.Done()
//		// using sleep to allow event processing
//		for i := 0; i < nEvents; i++ {
//			e.Notify("inc", testData{i: int64(1)})
//			if i%10 == 0 {
//				time.Sleep(25 * time.Millisecond)
//			}
//		}
//		time.Sleep(5000 * time.Millisecond)
//		e.Notify("end", testData{i: int64(nEvents)})
//	}()
//	wg.Add(1)
//	go func() {
//		defer wg.Done()
//		// using sleep to allow event processing
//		time.Sleep(50 * time.Millisecond)
//		for i := 0; i < nEvents; i++ {
//			e.Notify("inc", testData{i: int64(1)})
//			if i%5 == 0 {
//				time.Sleep(20 * time.Millisecond)
//			}
//		}
//		time.Sleep(2000 * time.Millisecond)
//		e.Notify("end", testData{i: int64(nEvents)})
//	}()
//
//	wg.Add(1)
//	go func() {
//		ended := int64(0)
//		var deregister DeregisterFunc
//		deregister = e.On("end", func(data EventData) {
//			parsed, ok := data.(testData)
//			require.True(t, ok)
//			atomic.AddInt64(&ended, parsed.i)
//			if atomic.LoadInt64(&ended) == int64(nEvents*2) {
//				time.Sleep(1000 * time.Millisecond)
//				wg.Done()
//				go deregister()
//			}
//		})
//	}()
//
//	wg.Wait()
//
//	for i := 0; i < n; i++ {
//		require.Equal(t, int64(nEvents*2), numbers[i], "wrong value in index %d", i)
//	}
//}
