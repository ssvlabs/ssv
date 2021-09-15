package tasks

import (
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"sync/atomic"
	"testing"
)

func TestRetry(t *testing.T) {
	var i int64

	inc := func() error {
		atomic.AddInt64(&i, 1)
		if i < 3 {
			return errors.New("test-error")
		}
		return nil
	}

	require.Nil(t, Retry(inc, 4))
	atomic.StoreInt64(&i, 0)
	require.EqualError(t, Retry(inc, 2), "test-error")
}
