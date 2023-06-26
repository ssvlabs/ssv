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
	eth1Client       eth1Client
	eventBatcher     eventBatcher
	eventDataHandler eventDataHandler

	logger     *zap.Logger
	metrics    metrics
	nodeProber nodeProber
}

func New(eth1Client eth1Client, eventBatcher eventBatcher, eventDataHandler eventDataHandler) *EventDispatcher {
	return &EventDispatcher{
		eth1Client:       eth1Client,
		eventBatcher:     eventBatcher,
		eventDataHandler: eventDataHandler,

		logger:     zap.NewNop(),
		metrics:    nopMetrics{},
		nodeProber: nil,
	}
}

// Start starts EventDispatcher.
// It fetches historical logs since fromBlock and passes them for processing.
// Then it asynchronously runs a loop which retrieves data from Eth1Client event stream and passes them for processing.
// Start blocks until historical logs are processed.
func (ed *EventDispatcher) Start(ctx context.Context, fromBlock uint64) error {
	if ed.nodeProber != nil {
		ready, err := ed.nodeProber.IsReady(ctx)
		if err != nil {
			return fmt.Errorf("check node readiness: %w", err)
		}

		if !ready {
			return ErrNodeNotReady
		}
	}

	logs, lastBlock, err := ed.eth1Client.FetchHistoricalLogs(ctx, fromBlock)
	if err != nil {
		return err
	}

	blockEvents := ed.eventBatcher.BatchHistoricalEvents(logs)
	if err := ed.eventDataHandler.HandleBlockEventsStream(blockEvents); err != nil {
		return fmt.Errorf("handle historical block events: %w", err)
	}

	go func() {
		logsStream := ed.eth1Client.StreamLogs(ctx, lastBlock+1)
		blockEventsStream := ed.eventBatcher.BatchOngoingEvents(logsStream)
		if err := ed.eventDataHandler.HandleBlockEventsStream(blockEventsStream); err != nil {
			// TODO: think how to handle this
			ed.logger.Error("failed to handle ongoing block event", zap.Error(err))
			return
		}
	}()

	return nil
}
