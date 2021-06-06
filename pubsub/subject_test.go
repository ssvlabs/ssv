package pubsub

import (
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
)

func TestSubject_Register_MultipleObservers(t *testing.T) {
	s := NewSubject()
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
	s := NewSubject()
	var wg sync.WaitGroup

	wg.Add(1)
	_, err := s.Register("test-observer1")
	require.NoError(t, err)
	_, err = s.Register("test-observer2")
	require.NoError(t, err)
	require.Equal(t, 2, len(s.(*subject).observers))
	observer := s.(*subject).observers["test-observer2"]
	s.Deregister("test-observer2")
	require.Equal(t, 1, len(s.(*subject).observers))
	require.False(t, observer.active)
}
