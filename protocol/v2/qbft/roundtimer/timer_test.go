package roundtimer

import (
	"context"
	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"sync/atomic"
	"testing"
	"time"
)

func TestRoundTimer_TimeoutForRound(t *testing.T) {
	count := int32(0)
	onTimeout := func() {
		atomic.AddInt32(&count, 1)
	}
	timer := New(context.Background(), zap.L(), onTimeout)
	timer.roundTimeout = DefaultRoundTimeout(1.1)
	timer.TimeoutForRound(qbft.Round(1))
	require.Equal(t, int32(0), atomic.LoadInt32(&count))
	<-time.After(timer.roundTimeout(qbft.Round(1)) + time.Millisecond*10)
	require.Equal(t, int32(1), atomic.LoadInt32(&count))

	atomic.StoreInt64(&timer.round, int64(0))
	timer.TimeoutForRound(qbft.Round(1))
	require.Equal(t, int32(1), atomic.LoadInt32(&count))
	<-time.After(timer.roundTimeout(qbft.Round(1)) + time.Millisecond*10)
	require.Equal(t, int32(2), atomic.LoadInt32(&count))
}
