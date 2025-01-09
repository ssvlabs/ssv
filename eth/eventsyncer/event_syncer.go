// Package eventsyncer implements functions for syncing registry contract events
// by reading them using executionclient and processing them using eventhandler.
package eventsyncer

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/logging/fields"
	nodestorage "github.com/ssvlabs/ssv/operator/storage"
)

// TODO: check if something from these PRs need to be ported:
// https://github.com/ssvlabs/ssv/pull/1053

var (
	// ErrNodeNotReady is returned when node is not ready.
	ErrNodeNotReady = fmt.Errorf("node not ready")
)

type ExecutionClient interface {
	FetchHistoricalLogs(ctx context.Context, fromBlock uint64) (logs <-chan executionclient.BlockLogs, errors <-chan error, err error)
	StreamLogs(ctx context.Context, fromBlock uint64) <-chan executionclient.BlockLogs
}

type EventHandler interface {
	HandleBlockEventsStream(ctx context.Context, logs <-chan executionclient.BlockLogs, executeTasks bool) (uint64, error)
}

// EventSyncer syncs registry contract events from the given ExecutionClient
// and passes them to the given EventHandler for processing.
type EventSyncer struct {
	nodeStorage     nodestorage.Storage
	executionClient ExecutionClient
	eventHandler    EventHandler

	logger             *zap.Logger
	stalenessThreshold time.Duration

	lastProcessedBlock       uint64
	lastProcessedBlockChange time.Time
}

func New(nodeStorage nodestorage.Storage, executionClient ExecutionClient, eventHandler EventHandler, opts ...Option) *EventSyncer {
	es := &EventSyncer{
		nodeStorage:     nodeStorage,
		executionClient: executionClient,
		eventHandler:    eventHandler,

		logger:             zap.NewNop(),
		stalenessThreshold: 150 * time.Second,
	}

	for _, opt := range opts {
		opt(es)
	}

	return es
}

// Healthy returns nil if the syncer is syncing ongoing events.
func (es *EventSyncer) Healthy(ctx context.Context) error {
	lastProcessedBlock, found, err := es.nodeStorage.GetLastProcessedBlock(nil)
	if err != nil {
		return fmt.Errorf("failed to read last processed block: %w", err)
	}
	if !found || lastProcessedBlock == nil || lastProcessedBlock.Uint64() == 0 {
		return fmt.Errorf("last processed block is not set")
	}
	if es.lastProcessedBlock != lastProcessedBlock.Uint64() {
		es.lastProcessedBlock = lastProcessedBlock.Uint64()
		es.lastProcessedBlockChange = time.Now()
		return nil
	}
	if time.Since(es.lastProcessedBlockChange) > es.stalenessThreshold {
		return fmt.Errorf("syncing is stuck at block %d", lastProcessedBlock.Uint64())
	}

	highestSeenBlock, found, err := es.nodeStorage.GetHighestSeenBlock(nil)
	if err != nil {
		return fmt.Errorf("failed to read last processed block: %w", err)
	}
	if !found || highestSeenBlock == nil || highestSeenBlock.Uint64() == 0 {
		return fmt.Errorf("last processed block is not set")
	}

	// Check if the block is too old.
	header, err := es.executionClient.(*executionclient.ExecutionClient).HeaderByNumber(ctx, highestSeenBlock)
	if err != nil {
		return fmt.Errorf("failed to get header for block %d: %w", es.lastProcessedBlock, err)
	}

	headerTime := time.Unix(int64(header.Time), 0)
	threashold := time.Now().Add(-es.stalenessThreshold)

	if headerTime.Before(threashold) {
		return fmt.Errorf("block %d is too old", es.lastProcessedBlock)
	}

	return nil
}

// SyncHistory reads and processes historical events since the given fromBlock.
func (es *EventSyncer) SyncHistory(ctx context.Context, fromBlock uint64) (lastProcessedBlock uint64, err error) {
	fetchLogs, fetchError, err := es.executionClient.FetchHistoricalLogs(ctx, fromBlock)
	if errors.Is(err, executionclient.ErrNothingToSync) {
		// Nothing to sync, should keep ongoing sync from the given fromBlock.
		return 0, executionclient.ErrNothingToSync
	}
	if err != nil {
		return 0, fmt.Errorf("failed to fetch historical events: %w", err)
	}

	lastProcessedBlock, err = es.eventHandler.HandleBlockEventsStream(ctx, fetchLogs, false)
	if err != nil {
		return 0, fmt.Errorf("handle historical block events: %w", err)
	}
	// TODO: (Alan) should it really be here?
	if err := <-fetchError; err != nil {
		return 0, fmt.Errorf("error occurred while fetching historical logs: %w", err)
	}
	if lastProcessedBlock == 0 {
		return 0, fmt.Errorf("handle historical block events: lastProcessedBlock is 0")
	}
	if lastProcessedBlock < fromBlock {
		// Event replay: this should never happen!
		return 0, fmt.Errorf("event replay: lastProcessedBlock (%d) is lower than fromBlock (%d)", lastProcessedBlock, fromBlock)
	}

	// Check if the block is too old.
	header, err := es.executionClient.(*executionclient.ExecutionClient).HeaderByNumber(ctx, big.NewInt(int64(es.lastProcessedBlock)))
	if err != nil {
		return 0, fmt.Errorf("failed to get header for block %d: %w", es.lastProcessedBlock, err)
	}
	if header.Time < uint64(time.Now().Add(-3*time.Minute).Unix()) {
		return 0, fmt.Errorf("block %d is too old", es.lastProcessedBlock)
	}

	es.logger.Info("finished syncing historical events",
		zap.Uint64("from_block", fromBlock),
		zap.Uint64("last_processed_block", lastProcessedBlock))

	return lastProcessedBlock, nil
}

// SyncOngoing streams and processes ongoing events as they come since the given fromBlock.
func (es *EventSyncer) SyncOngoing(ctx context.Context, fromBlock uint64) error {
	es.logger.Info("subscribing to ongoing registry events", fields.FromBlock(fromBlock))

	logStream := es.executionClient.StreamLogs(ctx, fromBlock)
	_, err := es.eventHandler.HandleBlockEventsStream(ctx, logStream, true)
	return err
}
