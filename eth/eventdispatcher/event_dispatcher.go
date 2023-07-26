package eventdispatcher

import (
	"context"
	"fmt"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/eth/eventbatcher"
	"github.com/bloxapp/ssv/logging/fields"
)

// TODO: check if something from these PRs need to be ported:
// https://github.com/bloxapp/ssv/pull/1053

var (
	// ErrNodeNotReady is returned when node is not ready.
	ErrNodeNotReady = fmt.Errorf("node not ready")
)

// TODO: still working on it
// type Event struct {
// 	BlockNumber uint64
// 	Logs        []*ethtypes.Log
// 	Err         error
// }

type executionClient interface {
	FetchHistoricalLogs(ctx context.Context, fromBlock uint64) (logs <-chan ethtypes.Log, errors <-chan error, err error)
	StreamLogs(ctx context.Context, fromBlock uint64) <-chan ethtypes.Log
}

type eventBatcher interface {
	BatchEvents(events <-chan ethtypes.Log) <-chan eventbatcher.BlockEvents
}

type eventDataHandler interface {
	HandleBlockEventsStream(blockEvents <-chan eventbatcher.BlockEvents, executeTasks bool) (uint64, error)
}

type nodeProber interface {
	IsReady(ctx context.Context) (bool, error)
}

type EventDispatcher struct {
	executionClient  executionClient
	eventBatcher     eventBatcher
	eventDataHandler eventDataHandler

	logger     *zap.Logger
	metrics    metrics
	nodeProber nodeProber
}

func New(executionClient executionClient, eventBatcher eventBatcher, eventDataHandler eventDataHandler, opts ...Option) *EventDispatcher {
	ed := &EventDispatcher{
		executionClient:  executionClient,
		eventBatcher:     eventBatcher,
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
	if err != nil {
		return 0, fmt.Errorf("failed to fetch historical events: %w", err)
	}

	blockEvents := ed.eventBatcher.BatchEvents(fetchLogs)
	lastProcessedBlock, err = ed.eventDataHandler.HandleBlockEventsStream(blockEvents, false)
	if err != nil {
		return 0, fmt.Errorf("handle historical block events: %w", err)
	}
	ed.metrics.LastBlockProcessed(lastProcessedBlock)

	if err := <-fetchError; err != nil {
		return 0, fmt.Errorf("error occurred while fetching historical logs: %w", err)
	}

	ed.logger.Info("finished syncing historical events",
		zap.Uint64("last_processed_block", lastProcessedBlock))
	return

	// TODO: log shares, operators, my validators
	//shares := n.storage.Shares().List()
	//operators, err := n.storage.ListOperators(logger, 0, 0)
	//if err != nil {
	//	logger.Error("failed to get operators", zap.Error(err))
	//}
	//operatorID := n.validatorsCtrl.GetOperatorData().ID
	//operatorValidatorsCount := 0
	//if operatorID != 0 {
	//	for _, share := range shares {
	//		if share.BelongsToOperator(operatorID) {
	//			operatorValidatorsCount++
	//		}
	//	}
	//}
	//
	//ed.logger.Info("ETH1 sync history stats",
	//	zap.Int("validators count", len(shares)),
	//	zap.Int("operators count", len(operators)),
	//	zap.Int("my validators count", operatorValidatorsCount),
	//)
}

// SyncOngoing runs a loop which retrieves data from ExecutionClient event stream and passes them for processing.
func (ed *EventDispatcher) SyncOngoing(ctx context.Context, fromBlock uint64) error {
	ed.logger.Info("subscribing to ongoing registry events", fields.FromBlock(fromBlock))

	logsStream := ed.executionClient.StreamLogs(ctx, fromBlock)
	blockEventsStream := ed.eventBatcher.BatchEvents(logsStream)
	_, err := ed.eventDataHandler.HandleBlockEventsStream(blockEventsStream, true)
	return err
}
