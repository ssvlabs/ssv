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
	"github.com/ethereum/go-ethereum/rpc"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/observability/log/fields"
	nodestorage "github.com/ssvlabs/ssv/operator/storage"
)

//go:generate go tool -modfile=../../tool.mod mockgen -package=eventsyncer -destination=./event_syncer_mock.go -source=./event_syncer.go

const (
	defaultStalenessThreshold = 300 * time.Second
)

type ExecutionClient interface {
	FetchHistoricalLogs(ctx context.Context, fromBlock uint64) (logsCh <-chan executionclient.BlockLogs, errsCh <-chan error, err error)
	StreamLogs(ctx context.Context, fromBlock uint64) (logsCh chan executionclient.BlockLogs)
	HeaderByNumber(ctx context.Context, blockNumber *big.Int) (*types.Header, error)
	IsFinalizedFork(ctx context.Context) bool
}

type EventHandler interface {
	HandleBlockEventsStream(
		ctx context.Context,
		logStreamCh <-chan executionclient.BlockLogs,
		executeTasks bool,
	) (lastProcessedBlock uint64, progressed bool, err error)
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

// Healthy determines if the EventSyncer is functioning correctly. It returns nil if healthy.
// It checks if the syncer has processed new blocks, hasn't been "stuck" without progress for too long
// (this check is skipped in finalized mode), and if its last processed block
// is sufficiently close to the relevant chain head (current time or EL finalized block time).
func (es *EventSyncer) Healthy(ctx context.Context) error {
	lastProcessedBlock, found, err := es.nodeStorage.GetLastProcessedBlock(nil)
	if err != nil {
		return fmt.Errorf("failed to read last processed block: %w", err)
	}
	if !found || lastProcessedBlock == nil || lastProcessedBlock.Uint64() == 0 {
		return fmt.Errorf("last processed block is not set")
	}
	lastBlockNum := lastProcessedBlock.Uint64()
	if es.lastProcessedBlock != lastBlockNum {
		es.lastProcessedBlock = lastBlockNum
		es.lastProcessedBlockChange = time.Now()
		return nil
	}

	// Check if we're making progress (only relevant for follow-distance approach)
	if !es.executionClient.IsFinalizedFork(ctx) {
		if time.Since(es.lastProcessedBlockChange) > es.stalenessThreshold {
			return fmt.Errorf("syncing is stuck at block %d", lastProcessedBlock.Uint64())
		}
	}

	// Check if our current position is too far behind
	return es.ensureBlockAboveThreshold(ctx, lastProcessedBlock)
}

// ensureBlockAboveThreshold checks if the given block is acceptably recent. Returns nil if fresh.
// Pre-finality, it compares the block's time against `time.Now()` using `es.stalenessThreshold`.
// Post-finality, it compares against the EL's finalized block time. If the given block is newer
// than the EL's finalized block, it's considered fresh (waiting for EL finality).
func (es *EventSyncer) ensureBlockAboveThreshold(ctx context.Context, block *big.Int) error {
	header, err := es.executionClient.HeaderByNumber(ctx, block)
	if err != nil {
		// Ensure block number is used in error message if that's the intent for %d
		return fmt.Errorf("failed to get header for block %d: %w", block.Uint64(), err)
	}

	var referenceTime time.Time
	if es.executionClient.IsFinalizedFork(ctx) {
		// Post-fork: Compare against finalized block time
		finalizedHeader, err := es.executionClient.HeaderByNumber(ctx, big.NewInt(rpc.FinalizedBlockNumber.Int64()))
		if err != nil {
			return fmt.Errorf("failed to get finalized block header: %w", err)
		}

		// If we're ahead of finalized, we're not behind - just waiting
		if finalizedHeader.Number.Uint64() < block.Uint64() {
			return nil
		}

		// #nosec G115
		referenceTime = time.Unix(int64(finalizedHeader.Time), 0)
	} else {
		// Pre-fork: Compare against current time
		referenceTime = time.Now()
	}

	// Check if block is older than threshold from reference time
	// #nosec G115
	blockTime := time.Unix(int64(header.Time), 0)
	if blockTime.Before(referenceTime.Add(-es.stalenessThreshold)) {
		return fmt.Errorf("block %d is too old (age: %s behind reference)",
			block.Uint64(), referenceTime.Sub(blockTime).Round(time.Second))
	}

	return nil
}

func (es *EventSyncer) syncHistory(ctx context.Context, fromBlock uint64) (
	lastProcessedBlock uint64,
	progressed bool,
	err error,
	retryable bool,
) {
	fetchLogsCh, fetchErrCh, err := es.executionClient.FetchHistoricalLogs(ctx, fromBlock)
	if errors.Is(err, executionclient.ErrNothingToSync) {
		// Nothing to sync, should keep ongoing sync from the given fromBlock.
		return 0, false, executionclient.ErrNothingToSync, false
	}
	if err != nil {
		return 0, false, fmt.Errorf("failed to fetch historical events: %w", err), true
	}

	// Process all the logs fetched until there are no more.
	lastProcessedBlock, progressed, err = es.eventHandler.HandleBlockEventsStream(ctx, fetchLogsCh, false)
	if err != nil {
		return lastProcessedBlock, progressed, fmt.Errorf("handle block events stream (last processed block = %d): %w", lastProcessedBlock, err), true
	}

	// Check there were no fetch-related errors.
	var errs error
	for err := range fetchErrCh {
		errs = errors.Join(errs, err)
	}
	if errs != nil {
		return lastProcessedBlock, progressed, fmt.Errorf("handle block events stream (last processed block = %d): %w", lastProcessedBlock, errs), true
	}

	// Sanity-check we are not replaying events - this should never happen!
	if lastProcessedBlock < fromBlock {
		return lastProcessedBlock, progressed, fmt.Errorf("event replay: lastProcessedBlock (%d) is lower than fromBlock (%d)", lastProcessedBlock, fromBlock), false
	}

	err = es.ensureBlockAboveThreshold(ctx, new(big.Int).SetUint64(lastProcessedBlock))
	if err != nil {
		return lastProcessedBlock, progressed, err, true
	}

	return lastProcessedBlock, progressed, nil, false
}

// SyncHistory reads and processes historical events since the given fromBlock.
func (es *EventSyncer) SyncHistory(ctx context.Context, fromBlock uint64) (lastProcessedBlock uint64, errs error) {
	const maxTries = 3
	for i := 0; i < maxTries; i++ {
		lpb, progressed, err, retryable := es.syncHistory(ctx, fromBlock)
		if err == nil {
			// Success, errors encountered so far (if any) don't matter, no need to log them.
			lastProcessedBlock = lpb
			return lastProcessedBlock, nil
		}

		// Encountered an error, let's see if we can retry it.
		errs = errors.Join(errs, fmt.Errorf("sync history (from_block=%d): %w", fromBlock, err))
		if !retryable {
			break
		}

		// Update the fromBlock, but only if we've got some progress (otherwise retry with previous fromBlock value).
		if progressed {
			fromBlock = lpb + 1
		}

		continue
	}

	return 0, fmt.Errorf("event syncer: couldn't sync history events: %w", errs)
}

// SyncOngoing streams and processes ongoing events as they come since the given fromBlock.
func (es *EventSyncer) SyncOngoing(ctx context.Context, fromBlock uint64) error {
	es.logger.Info("subscribing to ongoing registry events", fields.FromBlock(fromBlock))

	logStreamCh := es.executionClient.StreamLogs(ctx, fromBlock)
	lastProcessedBlock, progressed, err := es.eventHandler.HandleBlockEventsStream(ctx, logStreamCh, true)
	if err != nil {
		if progressed {
			return fmt.Errorf("handle block events stream (last processed block = %d): %w", lastProcessedBlock, err)
		}
		return fmt.Errorf("handle block events stream, couldn't progress at all: %w", err)
	}

	return nil
}
