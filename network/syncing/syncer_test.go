package syncing_test

import (
	"context"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
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
