package queue

import (
	"context"
	"testing"
	"time"

	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type FuncPrioritizer struct {
	priorFunc func(a, b *DecodedSSVMessage) bool
}

func (fp FuncPrioritizer) Prior(a, b *DecodedSSVMessage) bool {
	return fp.priorFunc(a, b)
}

func yourPrioritizerFunc(a, b *DecodedSSVMessage) bool {
	return true
}

func TestChannelUpdater(t *testing.T) {
	assert := assert.New(t)

	logger, _ := zap.NewProduction()
	defer logger.Sync()

	queue := New(32).(*priorityQueue)
	updater := NewChannelUpdater(logger, queue, FuncPrioritizer{priorFunc: yourPrioritizerFunc}, FilterAny)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	updater.Start(ctx)

	// Push messages into the queue
	for i := 0; i <= 5; i++ {
		decodeAndPush(t, queue, mockConsensusMessage{Height: qbft.Height(i), Type: qbft.PrepareMsgType}, mockState)
	}
	require.Equal(t, 5, queue.Len())

	time.Sleep(50 * time.Millisecond)

	for i := 0; i <= 5; i++ {
		select {
		case msg := <-updater.GetChannel():
			assert.NotNil(msg, "Expected message, got nil at index %d", i)
		case <-time.After(50 * time.Millisecond):
			assert.Fail("Did not receive message from updater in time")
		}
	}
	require.True(t, queue.Empty())
}

func TestChannelUpdater_EmptyQueue(t *testing.T) {
	assert := assert.New(t)

	logger, _ := zap.NewProduction()
	defer logger.Sync()

	queue := New(32).(*priorityQueue)
	updater := NewChannelUpdater(logger, queue, FuncPrioritizer{priorFunc: yourPrioritizerFunc}, FilterAny)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	updater.Start(ctx)

	time.Sleep(50 * time.Millisecond)

	select {
	case msg := <-updater.GetChannel():
		assert.Nil(msg, "Did not expect a message, but got one")
	case <-time.After(50 * time.Millisecond):
		// This is what we expect, no message
	}
}

func TestChannelUpdater_ContextCancellation(t *testing.T) {
	assert := assert.New(t)

	logger, _ := zap.NewProduction()
	defer logger.Sync()

	queue := New(32).(*priorityQueue)
	updater := NewChannelUpdater(logger, queue, FuncPrioritizer{priorFunc: yourPrioritizerFunc}, FilterAny)

	ctx, cancel := context.WithCancel(context.Background())

	updater.Start(ctx)
	cancel()

	time.Sleep(50 * time.Millisecond)

	_, open := <-updater.GetChannel()
	assert.False(open, "Expected channel to be closed after context cancellation")
}
