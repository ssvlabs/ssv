package syncing_test

import (
	"context"
	"testing"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/network/syncing"
)

type mockSyncer struct{}

func (m *mockSyncer) SyncHighestDecided(ctx context.Context, logger *zap.Logger, id spectypes.MessageID, handler syncing.MessageHandler) error {
	return nil
}

func (m *mockSyncer) SyncDecidedByRange(ctx context.Context, logger *zap.Logger, id spectypes.MessageID, from specqbft.Height, to specqbft.Height, handler syncing.MessageHandler) error {
	return nil
}

type mockMessageHandler struct {
	calls   int
	handler syncing.MessageHandler
}

func newMockMessageHandler() *mockMessageHandler {
	m := &mockMessageHandler{}
	m.handler = func(msg spectypes.SSVMessage) {
		m.calls++
	}
	return m
}

func TestThrottle(t *testing.T) {
	var calls int
	handler := syncing.Throttle(func(msg spectypes.SSVMessage) {
		calls++
	}, 10*time.Millisecond)

	start := time.Now()
	for i := 0; i < 10; i++ {
		handler(spectypes.SSVMessage{})
	}
	end := time.Now()

	require.Equal(t, 10, calls)

	rangeStart := start.Add(100 * time.Millisecond)
	rangeEnd := start.Add(120 * time.Millisecond)
	require.WithinRangef(t, end, rangeStart, rangeEnd, "expected duration to be between 100ms and 120ms, but it is %v", end.Sub(start))
}
