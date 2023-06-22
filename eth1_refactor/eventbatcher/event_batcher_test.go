package eventbatcher

import (
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

func TestEventBatcher_BatchHistoricalEvents(t *testing.T) {
	eb := NewEventBatcher()

	// Create sample events
	events := []ethtypes.Log{
		{
			BlockNumber: 1,
			TxHash:      ethcommon.Hash{1},
		},
		{
			BlockNumber: 1,
			TxHash:      ethcommon.Hash{2},
		},
		{
			BlockNumber: 2,
			TxHash:      ethcommon.Hash{3},
		},
	}

	expectedBlockEvents := []BlockEvents{
		{
			BlockNumber: 1,
			Events: []ethtypes.Log{
				{
					BlockNumber: 1,
					TxHash:      ethcommon.Hash{1},
				},
				{
					BlockNumber: 1,
					TxHash:      ethcommon.Hash{2},
				},
			},
		},
		{
			BlockNumber: 2,
			Events: []ethtypes.Log{
				{
					BlockNumber: 2,
					TxHash:      ethcommon.Hash{3},
				},
			},
		},
	}

	// Execute the BatchHistoricalEvents function
	result := make([]BlockEvents, 0)
	for blockEvent := range eb.BatchHistoricalEvents(events) {
		result = append(result, blockEvent)
	}

	require.Equal(t, expectedBlockEvents, result)
}

func TestEventBatcher_BatchOngoingEvents(t *testing.T) {
	eb := NewEventBatcher()

	// Create a channel to receive events
	eventsCh := make(chan ethtypes.Log)

	// Create sample events
	events := []ethtypes.Log{
		{
			BlockNumber: 1,
			// Set other relevant fields for the event
		},
		{
			BlockNumber: 1,
			// Set other relevant fields for the event
		},
		{
			BlockNumber: 2,
			// Set other relevant fields for the event
		},
	}

	expectedBlockEvents := []BlockEvents{
		{
			BlockNumber: 1,
			Events: []ethtypes.Log{
				{
					BlockNumber: 1,
					// Set other relevant fields for the event
				},
				{
					BlockNumber: 1,
					// Set other relevant fields for the event
				},
			},
		},
		{
			BlockNumber: 2,
			Events: []ethtypes.Log{
				{
					BlockNumber: 2,
					// Set other relevant fields for the event
				},
			},
		},
	}

	// Execute the BatchOngoingEvents function in a separate goroutine
	go func() {
		for _, event := range events {
			eventsCh <- event
		}
		close(eventsCh)
	}()

	// Execute the BatchOngoingEvents function
	result := make([]BlockEvents, 0)
	for blockEvent := range eb.BatchOngoingEvents(eventsCh) {
		result = append(result, blockEvent)
	}

	require.Equal(t, expectedBlockEvents, result)
}
