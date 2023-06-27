package eventbatcher

import (
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

func TestEventBatcher_BatchHistoricalEvents(t *testing.T) {
	eb := NewEventBatcher()

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

	result := make([]BlockEvents, 0)
	for blockEvent := range eb.BatchHistoricalEvents(events) {
		result = append(result, blockEvent)
	}

	require.Equal(t, expectedBlockEvents, result)
}

func TestEventBatcher_BatchOngoingEvents(t *testing.T) {
	eb := NewEventBatcher()

	eventsCh := make(chan ethtypes.Log)

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

	go func() {
		defer close(eventsCh)

		for _, event := range events {
			eventsCh <- event
		}
	}()

	result := make([]BlockEvents, 0)
	for blockEvent := range eb.BatchOngoingEvents(eventsCh) {
		result = append(result, blockEvent)
	}

	require.Equal(t, expectedBlockEvents, result)
}
