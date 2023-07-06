package eventdispatcher

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

var (
	// ErrNodeNotReady is returned when node is not ready.
	ErrNodeNotReady = fmt.Errorf("node not ready")
)

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

// Start starts EventDispatcher.
// It fetches historical logs since fromBlock and passes them for processing.
// Then it asynchronously runs a loop which retrieves data from Eth1Client event stream and passes them for processing.
// Start blocks until historical logs are processed.
func (ed *EventDispatcher) Start(ctx context.Context, fromBlock uint64) error {
	ed.logger.Info("starting event dispatcher")

	if ed.nodeProber != nil {
		ready, err := ed.nodeProber.IsReady(ctx)
		if err != nil {
			return fmt.Errorf("check node readiness: %w", err)
		}

		if !ready {
			return ErrNodeNotReady
		}
	}

	ed.logger.Info("going to fetch historical logs")
	logStream, errorStream, err := ed.executionClient.FetchHistoricalLogs(ctx, fromBlock)
	if err != nil {
		return fmt.Errorf("fetch historical logs: %w", err)
	}

	ed.logger.Info("going to batch historical logs")
	blockEvents := ed.eventBatcher.BatchEvents(logStream)

	ed.logger.Info("going to handle historical logs")
	lastProcessedBlock, err := ed.eventDataHandler.HandleBlockEventsStream(blockEvents, false)
	if err != nil {
		return fmt.Errorf("handle historical block events: %w", err)
	}
	ed.metrics.HistoricalLogsProcessed(lastProcessedBlock-fromBlock)

	if err := <-errorStream; err != nil {
		return fmt.Errorf("error occurred while fetching historical logs: %w", err)
	}

	ed.logger.Info("finished handling historical logs",
		zap.Uint64("last_processed_block", lastProcessedBlock))

	go func() {
		ed.logger.Info("going to handle ongoing logs")

		logsStream := ed.executionClient.StreamLogs(ctx, lastProcessedBlock+1)
		blockEventsStream := ed.eventBatcher.BatchEvents(logsStream)
		lastProcessedBlock, err := ed.eventDataHandler.HandleBlockEventsStream(blockEventsStream, true)
		if err != nil {
			// TODO: think how to handle this
			ed.logger.Error("failed to handle ongoing block event", zap.Error(err))
			ed.metrics.LogsProcessingError(err)
			return
		}
		ed.metrics.LastBlockProcessed(lastProcessedBlock)
		ed.logger.Info("finished handling ongoing logs",
			zap.Uint64("last_processed_block", lastProcessedBlock))
	}()

	return nil
}
