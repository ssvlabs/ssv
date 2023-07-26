package eventbatcher

import (
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
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

				currentBlockEvents = BlockEvents{
					BlockNumber: event.BlockNumber,
					Events:      []ethtypes.Log{event},
				}
			} else if event.BlockNumber < currentBlockEvents.BlockNumber {
				eb.logger.Fatal("received event from previous block, should never happen!",
					zap.Uint64("event_block", event.BlockNumber),
					zap.Uint64("current_block", currentBlockEvents.BlockNumber))
			} else {
				currentBlockEvents.Events = append(currentBlockEvents.Events, event)
			}
		}
		if len(currentBlockEvents.Events) != 0 {
			blockEvents <- currentBlockEvents
		}
	}()

	return blockEvents
}
