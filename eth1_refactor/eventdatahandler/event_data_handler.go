package eventdatahandler

import (
	"fmt"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/eth1_refactor/eventbatcher"
)

type EventDataHandler struct {
	eventDB      eventDB
	taskExecutor taskExecutor

	logger  *zap.Logger
	metrics metrics
}

func NewEventDataHandler(eventDB eventDB, taskExecutor taskExecutor) *EventDataHandler {
	return &EventDataHandler{
		eventDB:      eventDB,
		taskExecutor: taskExecutor,
		logger:       zap.NewNop(),
		metrics:      nopMetrics{},
	}
}

func (edh *EventDataHandler) HandleBlockEventsStream(blockEventsCh <-chan eventbatcher.BlockEvents) error {
	for blockEvents := range blockEventsCh {
		if err := edh.processBlockEvents(blockEvents); err != nil {
			return fmt.Errorf("process block events: %w", err)
		}

		if edh.taskExecutor != nil {
			edh.logger.Info("executing task")
			if err := edh.taskExecutor.ExecuteTasks(blockEvents.CreateTasks()...); err != nil {
				return fmt.Errorf("execute tasks")
			}
		}
	}

	return nil
}

func (edh *EventDataHandler) processBlockEvents(blockEvents eventbatcher.BlockEvents) error {
	edh.eventDB.BeginTx()
	defer edh.eventDB.EndTx()

	for _, event := range blockEvents.Events {
		if err := edh.processEvent(event); err != nil {
			return err
		}
	}

	return nil
}

func (edh *EventDataHandler) processEvent(event ethtypes.Log) error {
	// TODO: handle

	return nil
}
