package pubsub

import (
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSubject_Register_MultipleObservers(t *testing.T) {
	s := NewSubject(zap.L())
	var wg sync.WaitGroup

	wg.Add(1)
	cn1, err := s.Register("test-observer1")
	require.NoError(t, err)
	go func() {
		<-cn1
		wg.Done()
	}()

	wg.Add(1)
	cn2, err := s.Register("test-observer2")
	require.NoError(t, err)
	go func() {
		e := <-cn2
		require.Equal(t, "event", e)
		wg.Done()
	}()

	e := "event"
	s.Notify(e)

	wg.Wait()
}

func TestSubject_Deregister(t *testing.T) {
	s := NewSubject(zap.L())
	var wg1 sync.WaitGroup
	var wg2 sync.WaitGroup

	cn1, err := s.Register("test-observer1")
	require.NoError(t, err)
	go func() {
		for e := range cn1 {
			wg1.Done()
			s, ok := e.(string)
			require.True(t, ok)
			require.True(t, strings.Contains(s, "event"))
		}
	}()
	cn2, err := s.Register("test-observer2")
	require.NoError(t, err)
	go func() {
		for e := range cn2 {
			wg2.Done()
			require.Equal(t, "event", e)
		}
	}()
	require.Equal(t, 2, len(s.(*subject).observers))
	wg2.Add(1)
	wg1.Add(1)
	go s.Notify("event")

	wg2.Wait()

	ob := s.(*subject).observers["test-observer2"]
	s.Deregister("test-observer2")
	// sleeping 5ms as the observer will be closed in a new goroutine
	time.Sleep(5 * time.Millisecond)
	require.Equal(t, 1, len(s.(*subject).observers))
	require.False(t, ob.isActive())

	wg1.Add(1)
	s.Notify("event_xxx")

	wg1.Wait()
}

func TestSubject_ClosedChannel(t *testing.T) {
	s := NewSubject(zap.L())
	var wg sync.WaitGroup

	wg.Add(1)
	cn, err := s.Register("test-observer")
	require.NoError(t, err)
	go func() {
		for e := range cn {
			require.Equal(t, "event", e)
			wg.Done()
		}
	}()
	go s.Notify("event")
	ob := s.(*subject).observers["test-observer"]

	ob.mut.Lock()
	close(ob.channel)
	ob.mut.Unlock()

	// send on closed channel
	go s.Notify("event")
	time.Sleep(5 * time.Millisecond)
	// close of closed channel
	go s.Deregister("test-observer")
	time.Sleep(5 * time.Millisecond)
	// the observer should be inactive after deregister
	require.False(t, ob.isActive())
}