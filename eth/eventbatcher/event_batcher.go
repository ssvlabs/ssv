package eventbatcher

import (
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

const defaultEventBuf = 1024

type EventBatcher struct{}

func NewEventBatcher() *EventBatcher {
	return &EventBatcher{}
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
