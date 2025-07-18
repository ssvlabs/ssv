// Package eventsyncer implements functions for syncing registry contract events
// by reading them using executionclient and processing them using eventhandler.
package eventsyncer

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/logging/fields"
	nodestorage "github.com/ssvlabs/ssv/operator/storage"
)

//go:generate go tool -modfile=../../tool.mod mockgen -package=eventsyncer -destination=./event_syncer_mock.go -source=./event_syncer.go

// TODO: check if something from these PRs need to be ported:
// https://github.com/ssvlabs/ssv/pull/1053

const (
	defaultStalenessThreshold = 300 * time.Second
)

var (
	// ErrNodeNotReady is returned when node is not ready.
	ErrNodeNotReady = fmt.Errorf("node not ready")
)

type ExecutionClient interface {
	FetchHistoricalLogs(ctx context.Context, fromBlock uint64) (logs <-chan executionclient.BlockLogs, errors <-chan error, err error)
	StreamLogs(ctx context.Context, fromBlock uint64) <-chan executionclient.BlockLogs
	HeaderByNumber(ctx context.Context, blockNumber *big.Int) (*types.Header, error)
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
		stalenessThreshold: defaultStalenessThreshold,
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

	return es.blockBelowThreshold(ctx, lastProcessedBlock)
}

func (es *EventSyncer) blockBelowThreshold(ctx context.Context, block *big.Int) error {
	header, err := es.executionClient.HeaderByNumber(ctx, block)
	if err != nil {
		return fmt.Errorf("failed to get header for block %d: %w", block, err)
	}

	// #nosec G115
	if header.Time < uint64(time.Now().Add(-es.stalenessThreshold).Unix()) {
		return fmt.Errorf("block %d is too old", block)
	}

	return nil
}

// SyncHistory reads and processes historical events since the given fromBlock.
func (es *EventSyncer) SyncHistory(ctx context.Context, fromBlock uint64) (lastProcessedBlock uint64, err error) {
	const maxTries = 3
	var prevProcessedBlock uint64
	for i := 0; i < maxTries; i++ {
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

		if lastProcessedBlock == prevProcessedBlock {
			// Not advancing, so can't sync any further.
			break
		}
		prevProcessedBlock = lastProcessedBlock

		err = es.blockBelowThreshold(ctx, new(big.Int).SetUint64(lastProcessedBlock))
		if err == nil {
			// Successfully synced up to a fresh block.
			es.logger.Info("finished syncing historical events",
				zap.Uint64("from_block", fromBlock),
				zap.Uint64("last_processed_block", lastProcessedBlock))

			return lastProcessedBlock, nil
		}

		fromBlock = lastProcessedBlock + 1
		es.logger.Info("finished syncing up to a stale block, resuming", zap.Uint64("from_block", fromBlock))
	}

	return 0, fmt.Errorf("highest block is too old (%d)", lastProcessedBlock)
}

// SyncOngoing streams and processes ongoing events as they come since the given fromBlock.
func (es *EventSyncer) SyncOngoing(ctx context.Context, fromBlock uint64) error {
	es.logger.Info("subscribing to ongoing registry events", fields.FromBlock(fromBlock))

	logStream := es.executionClient.StreamLogs(ctx, fromBlock)
	_, err := es.eventHandler.HandleBlockEventsStream(ctx, logStream, true)
	return err
}
