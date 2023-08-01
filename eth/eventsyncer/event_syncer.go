package eventsyncer

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

type EventSyncer struct {
	executionClient  executionClient
	eventDataHandler eventDataHandler

	logger  *zap.Logger
	metrics metrics
}

func New(executionClient executionClient, eventDataHandler eventDataHandler, opts ...Option) *EventSyncer {
	es := &EventSyncer{
		executionClient:  executionClient,
		eventDataHandler: eventDataHandler,

		logger:  zap.NewNop(),
		metrics: nopMetrics{},
	}

	for _, opt := range opts {
		opt(es)
	}

	return es
}

// SyncHistory fetches historical logs since fromBlock and passes them for processing.
func (es *EventSyncer) SyncHistory(ctx context.Context, fromBlock uint64) (lastProcessedBlock uint64, err error) {
	fetchLogs, fetchError, err := es.executionClient.FetchHistoricalLogs(ctx, fromBlock)
	if errors.Is(err, executionclient.ErrNothingToSync) {
		// Nothing to sync, should keep ongoing sync from the given fromBlock.
		return 0, executionclient.ErrNothingToSync
	}
	if err != nil {
		return 0, fmt.Errorf("failed to fetch historical events: %w", err)
	}

	lastProcessedBlock, err = es.eventDataHandler.HandleBlockEventsStream(fetchLogs, false)
	if err != nil {
		return 0, fmt.Errorf("handle historical block events: %w", err)
	}
	if lastProcessedBlock == 0 {
		return 0, fmt.Errorf("handle historical block events: lastProcessedBlock is 0")
	}
	if lastProcessedBlock < fromBlock {
		// Event replay: this should never happen!
		return 0, fmt.Errorf("event replay: lastProcessedBlock (%d) is lower than fromBlock (%d)", lastProcessedBlock, fromBlock)
	}
	es.metrics.LastBlockProcessed(lastProcessedBlock)

	if err := <-fetchError; err != nil {
		return 0, fmt.Errorf("error occurred while fetching historical logs: %w", err)
	}

	es.logger.Info("finished syncing historical events",
		zap.Uint64("from_block", fromBlock),
		zap.Uint64("last_processed_block", lastProcessedBlock))
	return lastProcessedBlock, nil
}

// SyncOngoing runs a loop which retrieves data from ExecutionClient event stream and passes them for processing.
func (es *EventSyncer) SyncOngoing(ctx context.Context, fromBlock uint64) error {
	es.logger.Info("subscribing to ongoing registry events", fields.FromBlock(fromBlock))

	logs := es.executionClient.StreamLogs(ctx, fromBlock)
	_, err := es.eventDataHandler.HandleBlockEventsStream(logs, true)
	return err
}
