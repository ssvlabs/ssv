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
	"github.com/ssvlabs/ssv/observability/log/fields"
	nodestorage "github.com/ssvlabs/ssv/operator/storage"
)

//go:generate go tool -modfile=../../tool.mod mockgen -package=eventsyncer -destination=./event_syncer_mock.go -source=./event_syncer.go

const (
	defaultStalenessThreshold = 300 * time.Second
)

type ExecutionClient interface {
	FetchHistoricalLogs(ctx context.Context, fromBlock uint64, verify bool) (logsCh <-chan executionclient.BlockLogs, errsCh <-chan error, err error)
	VerifyLogs(ctx context.Context, fromBlock, toBlock uint64) ([]executionclient.BlockLogs, error)
	BlockContractLogs(ctx context.Context, block uint64) (logs []types.Log, receiptsAvailable bool, err error)
	StreamLogs(ctx context.Context, fromBlock uint64) (logsCh chan executionclient.BlockLogs)
	HeaderByNumber(ctx context.Context, blockNumber *big.Int) (*types.Header, error)
}

type EventHandler interface {
	HandleBlockEventsStream(
		ctx context.Context,
		logStreamCh <-chan executionclient.BlockLogs,
		executeTasks bool,
		journalFrom *uint64,
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

	verifyChunkSize         uint64
	verifyChunkDelay        time.Duration
	verifyRetryInitialDelay time.Duration
	verifyRetryMaxDelay     time.Duration
	resyncCooldown          time.Duration

	lastProcessedBlock       uint64
	lastProcessedBlockChange time.Time
}

func New(nodeStorage nodestorage.Storage, executionClient ExecutionClient, eventHandler EventHandler, opts ...Option) *EventSyncer {
	es := &EventSyncer{
		nodeStorage:     nodeStorage,
		executionClient: executionClient,
		eventHandler:    eventHandler,

		logger:                  zap.NewNop(),
		stalenessThreshold:      defaultStalenessThreshold,
		verifyChunkSize:         defaultVerifyChunkSize,
		verifyChunkDelay:        defaultVerifyChunkDelay,
		verifyRetryInitialDelay: defaultVerifyRetryInitialDelay,
		verifyRetryMaxDelay:     defaultVerifyRetryMaxDelay,
		resyncCooldown:          defaultResyncCooldown,
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

	return es.ensureBlockAboveThreshold(ctx, lastProcessedBlock)
}

func (es *EventSyncer) ensureBlockAboveThreshold(ctx context.Context, block *big.Int) error {
	header, err := es.executionClient.HeaderByNumber(ctx, block)
	if err != nil {
		return fmt.Errorf("failed to get header for block %d: %w", block, err)
	}

	// Compare in signed space: a staleness threshold large enough to push the cutoff before
	// the Unix epoch would wrap the uint64 cast and flag every block as too old.
	cutoff := time.Now().Add(-es.stalenessThreshold).Unix()
	// #nosec G115 -- block timestamps fit in int64
	if int64(header.Time) < cutoff {
		return fmt.Errorf("block %d is too old", block)
	}

	return nil
}

func (es *EventSyncer) syncHistory(ctx context.Context, fromBlock uint64, journalFrom *uint64, verify bool) (
	lastProcessedBlock uint64,
	progressed bool,
	err error,
	retryable bool,
) {
	fetchLogsCh, fetchErrCh, err := es.executionClient.FetchHistoricalLogs(ctx, fromBlock, verify)
	if errors.Is(err, executionclient.ErrNothingToSync) {
		// Nothing to sync, should keep ongoing sync from the given fromBlock.
		return 0, false, executionclient.ErrNothingToSync, false
	}
	if err != nil {
		return 0, false, fmt.Errorf("failed to fetch historical events: %w", err), true
	}

	// An optimistic sync passes journalFrom so the handler records verification bookkeeping as it
	// advances the marker; a verified resync passes nil (its batches are already checked inline).
	lastProcessedBlock, progressed, err = es.eventHandler.HandleBlockEventsStream(ctx, fetchLogsCh, false, journalFrom)
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

// SyncHistory reads and processes historical events since the given fromBlock. When verify is
// false (the normal path) the sync is optimistic and fast: the handler journals an unverified
// range and per-block digests as it advances the marker, for the background verifier to check
// for completeness later. When true (the resync-repair path) each batch is verified inline, so
// the result is guaranteed complete and nothing is journalled.
func (es *EventSyncer) SyncHistory(ctx context.Context, fromBlock uint64, verify bool) (lastProcessedBlock uint64, errs error) {
	// journalFrom keys the unverified range recorded during an optimistic sync; it stays fixed at
	// the original start across retries so the range spans the whole optimistic span. A verified
	// resync journals nothing (nil). It's a pointer, not a 0 sentinel, so an offset-0 network
	// (RegistrySyncOffset == 0) still journals block 0 instead of silently skipping verification.
	// Journaling happens in the handler's marker transaction, so a crash can't advance the marker
	// past a block the verifier won't later check.
	var journalFrom *uint64
	if !verify {
		from := fromBlock
		journalFrom = &from
	}

	const maxTries = 3
	for i := 0; i < maxTries; i++ {
		lpb, progressed, err, retryable := es.syncHistory(ctx, fromBlock, journalFrom, verify)
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

	// Streaming is verified inline (StreamLogs cross-checks each batch), so no verification
	// bookkeeping is journalled (journalFrom=nil) and no background verification is needed.
	logStreamCh := es.executionClient.StreamLogs(ctx, fromBlock)
	lastProcessedBlock, progressed, err := es.eventHandler.HandleBlockEventsStream(ctx, logStreamCh, true, nil)
	if err != nil {
		if progressed {
			return fmt.Errorf("handle block events stream (last processed block = %d): %w", lastProcessedBlock, err)
		}
		return fmt.Errorf("handle block events stream, couldn't progress at all: %w", err)
	}

	return nil
}
