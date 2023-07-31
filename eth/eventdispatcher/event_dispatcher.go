package eventdispatcher

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/eth/executionclient"
	"github.com/bloxapp/ssv/logging/fields"
)

// TODO: check if something from these PRs need to be ported:
// https://github.com/bloxapp/ssv/pull/1053

var (
	// ErrNodeNotReady is returned when node is not ready.
	ErrNodeNotReady = fmt.Errorf("node not ready")
)

type executionClient interface {
	FetchHistoricalLogs(ctx context.Context, fromBlock uint64) (logs <-chan executionclient.BlockLogs, errors <-chan error, err error)
	StreamLogs(ctx context.Context, fromBlock uint64) <-chan executionclient.BlockLogs
}

type eventDataHandler interface {
	HandleBlockEventsStream(logs <-chan executionclient.BlockLogs, executeTasks bool) (uint64, error)
}

type nodeProber interface {
	IsReady(ctx context.Context) (bool, error)
}

type EventDispatcher struct {
	executionClient  executionClient
	eventDataHandler eventDataHandler

	logger     *zap.Logger
	metrics    metrics
	nodeProber nodeProber
}

func New(executionClient executionClient, eventDataHandler eventDataHandler, opts ...Option) *EventDispatcher {
	ed := &EventDispatcher{
		executionClient:  executionClient,
		eventDataHandler: eventDataHandler,

		logger:     zap.NewNop(),
		metrics:    nopMetrics{},
		nodeProber: nil,
	}

	for _, opt := range opts {
		opt(ed)
	}

	return ed
}

// SyncHistory fetches historical logs since fromBlock and passes them for processing.
func (ed *EventDispatcher) SyncHistory(ctx context.Context, fromBlock uint64) (lastProcessedBlock uint64, err error) {
	if ed.nodeProber != nil {
		ready, err := ed.nodeProber.IsReady(ctx)
		if err != nil {
			return 0, fmt.Errorf("check node readiness: %w", err)
		}
		if !ready {
			return 0, ErrNodeNotReady
		}
	}

	fetchLogs, fetchError, err := ed.executionClient.FetchHistoricalLogs(ctx, fromBlock)
	if errors.Is(err, executionclient.ErrNothingToSync) {
		// Nothing to sync, should keep ongoing sync from the given fromBlock.
		return fromBlock, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to fetch historical events: %w", err)
	}

	lastProcessedBlock, err = ed.eventDataHandler.HandleBlockEventsStream(fetchLogs, false)
	if err != nil {
		return 0, fmt.Errorf("handle historical block events: %w", err)
	}
	if lastProcessedBlock == 0 {
		// No events were processed, so lastProcessedBlock remains fromBlock.
		lastProcessedBlock = fromBlock
	} else if lastProcessedBlock < fromBlock {
		// Event replay: this should never happen!
		return 0, fmt.Errorf("event replay: lastProcessedBlock (%d) is lower than fromBlock (%d)", lastProcessedBlock, fromBlock)
	}
	ed.metrics.LastBlockProcessed(lastProcessedBlock)

	if err := <-fetchError; err != nil {
		return 0, fmt.Errorf("error occurred while fetching historical logs: %w", err)
	}

	ed.logger.Info("finished syncing historical events",
		zap.Uint64("from_block", fromBlock),
		zap.Uint64("last_processed_block", lastProcessedBlock))
	return lastProcessedBlock, nil
}

// SyncOngoing runs a loop which retrieves data from ExecutionClient event stream and passes them for processing.
func (ed *EventDispatcher) SyncOngoing(ctx context.Context, fromBlock uint64) error {
	ed.logger.Info("subscribing to ongoing registry events", fields.FromBlock(fromBlock))

	logs := ed.executionClient.StreamLogs(ctx, fromBlock)
	_, err := ed.eventDataHandler.HandleBlockEventsStream(logs, true)
	return err
}
