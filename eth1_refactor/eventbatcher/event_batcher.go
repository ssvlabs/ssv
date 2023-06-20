package eventbatcher

import (
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

type EventBatcher struct{}

func NewEventBatcher() *EventBatcher {
	return &EventBatcher{}
}

// TODO: reduce code duplication between BatchHistoricalEvents and BatchOngoingEvents.

func (eb *EventBatcher) BatchHistoricalEvents(events []ethtypes.Log) <-chan BlockEvents {
	blockEvents := make(chan BlockEvents)
	go func() {
		defer close(blockEvents)

		var currentBlockEvents BlockEvents

		for _, event := range events {
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
	}()

	return blockEvents
}

func (eb *EventBatcher) BatchOngoingEvents(events <-chan ethtypes.Log) <-chan BlockEvents {
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
	}()

	return blockEvents
}
