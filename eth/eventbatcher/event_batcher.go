package eventbatcher

import (
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

type EventBatcher struct{}

func NewEventBatcher() *EventBatcher {
	return &EventBatcher{}
}

type BlockEvents struct {
	BlockNumber uint64
	Events      []ethtypes.Log
}

func (eb *EventBatcher) BatchEvents(events <-chan ethtypes.Log) <-chan BlockEvents {
	blockEvents := make(chan BlockEvents)
	go func() {
		defer close(blockEvents)
		var currentBlockEvents BlockEvents
		for event := range events {
			if currentBlockEvents.BlockNumber == 0 {
				currentBlockEvents.BlockNumber = event.BlockNumber
				currentBlockEvents.Events = []ethtypes.Log{event}
				continue
			}

			if event.BlockNumber > currentBlockEvents.BlockNumber {
				blockEvents <- currentBlockEvents

				currentBlockEvents.BlockNumber = event.BlockNumber
				currentBlockEvents.Events = []ethtypes.Log{event}
				continue
			}
			currentBlockEvents.Events = append(currentBlockEvents.Events, event)
		}
		if len(currentBlockEvents.Events) != 0 {
			blockEvents <- currentBlockEvents
		}
	}()

	return blockEvents
}
