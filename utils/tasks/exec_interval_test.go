package tasks

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExecWithInterval(t *testing.T) {
	var list []string
	var mut sync.Mutex

	addToList := func(_ time.Duration) (bool, bool) {
		mut.Lock()
		defer mut.Unlock()

		list = append(list, time.Now().String())

		if len(list) < 10 {
			return false, false
		}
		if len(list) == 5 {
			return false, true
		}
		if len(list) == 10 {
			return true, false
		}
		return false, false
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ExecWithInterval(t.Context(), addToList, 20*time.Millisecond, time.Second)
	}()
	wg.Wait()
	require.Equal(t, 10, len(list))
}
