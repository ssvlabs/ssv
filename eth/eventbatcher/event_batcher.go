package eventbatcher

import (
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

type EventBatcher struct{}

func NewEventBatcher() *EventBatcher {
	return &EventBatcher{}
}

func (eb *EventBatcher) BatchEvents(events <-chan ethtypes.Log) <-chan BlockEvents {
	blockEvents := make(chan BlockEvents)
	go func() {
		defer close(blockEvents)
		var currentBlockEvents BlockEvents
		for event := range events {
			processEvents(event, &currentBlockEvents, blockEvents)
		}
		if len(currentBlockEvents.Events) != 0 {
			blockEvents <- currentBlockEvents
		}
	}()

	return blockEvents
}

func processEvents(event ethtypes.Log, currentBlockEvents *BlockEvents, blockEvents chan BlockEvents) {
	if currentBlockEvents.BlockNumber == 0 {
		currentBlockEvents.BlockNumber = event.BlockNumber
		currentBlockEvents.Events = []ethtypes.Log{event}
		return
	}

	if event.BlockNumber > currentBlockEvents.BlockNumber {
		blockEvents <- *currentBlockEvents

		currentBlockEvents.BlockNumber = event.BlockNumber
		currentBlockEvents.Events = []ethtypes.Log{event}
		return
	}

	currentBlockEvents.Events = append(currentBlockEvents.Events, event)
}
