package eventbatcher

import (
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
)

const defaultEventBuf = 1024

type EventBatcher struct {
	logger *zap.Logger
}

func NewEventBatcher(options ...Option) *EventBatcher {
	eb := &EventBatcher{
		logger: zap.NewNop(),
	}

	for _, opt := range options {
		opt(eb)
	}

	return eb
}

type BlockEvents struct {
	BlockNumber uint64
	Events      []ethtypes.Log
}

func (eb *EventBatcher) BatchEvents(events <-chan ethtypes.Log) <-chan BlockEvents {
	eb.logger.Debug("starting event batching")

	blockEvents := make(chan BlockEvents, defaultEventBuf)
	go func() {
		defer close(blockEvents)

		var currentBlockEvents BlockEvents
		for event := range events {
			if currentBlockEvents.BlockNumber == 0 {
				currentBlockEvents.BlockNumber = event.BlockNumber
			}

			if event.BlockNumber > currentBlockEvents.BlockNumber {
				blockEvents <- currentBlockEvents
				eb.logger.Debug("batched block events", fields.BlockNumber(currentBlockEvents.BlockNumber))

				currentBlockEvents = BlockEvents{
					BlockNumber: event.BlockNumber,
					Events:      []ethtypes.Log{event},
				}
			} else {
				currentBlockEvents.Events = append(currentBlockEvents.Events, event)
			}
		}
		if len(currentBlockEvents.Events) != 0 {
			blockEvents <- currentBlockEvents
			eb.logger.Debug("batched block events", fields.BlockNumber(currentBlockEvents.BlockNumber))
		}

		eb.logger.Debug("finished event batching")
	}()

	return blockEvents
}
