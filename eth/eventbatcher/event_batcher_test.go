package eventbatcher_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/eth/eventbatcher"
)

// TODO: add a test that makes sure our batch size limit is enforced

func TestEventBatcher_BatchEvents(t *testing.T) {
	t.Run("NoEvents", func(t *testing.T) {
		eb := eventbatcher.NewEventBatcher()

		events := make(chan ethtypes.Log)

		blockEventsCh := eb.BatchEvents(events)

		close(events)

		_, more := <-blockEventsCh
		require.False(t, more)
	})

	t.Run("SingleBlockEvents", func(t *testing.T) {
		eb := eventbatcher.NewEventBatcher()

		events := make(chan ethtypes.Log)

		blockEventsCh := eb.BatchEvents(events)

		event1 := ethtypes.Log{
			BlockNumber: 1,
			Address:     common.Address{},
			Topics:      []common.Hash{},
			Data:        []byte{},
		}
		event2 := ethtypes.Log{
			BlockNumber: 1,
			Address:     common.Address{},
			Topics:      []common.Hash{},
			Data:        []byte{},
		}
		event3 := ethtypes.Log{
			BlockNumber: 1,
			Address:     common.Address{},
			Topics:      []common.Hash{},
			Data:        []byte{},
		}

		events <- event1
		events <- event2
		events <- event3

		close(events)

		blockEvents := <-blockEventsCh

		require.Equal(t, uint64(1), blockEvents.BlockNumber)
		require.Equal(t, []ethtypes.Log{event1, event2, event3}, blockEvents.Events)

		_, more := <-blockEventsCh
		require.False(t, more)
	})

	t.Run("MultipleBlockEvents", func(t *testing.T) {
		eb := eventbatcher.NewEventBatcher()

		events := make(chan ethtypes.Log)

		blockEventsCh := eb.BatchEvents(events)

		event1 := ethtypes.Log{
			BlockNumber: 1,
			Address:     common.Address{},
			Topics:      []common.Hash{},
			Data:        []byte{},
		}
		event2 := ethtypes.Log{
			BlockNumber: 2,
			Address:     common.Address{},
			Topics:      []common.Hash{},
			Data:        []byte{},
		}
		event3 := ethtypes.Log{
			BlockNumber: 3,
			Address:     common.Address{},
			Topics:      []common.Hash{},
			Data:        []byte{},
		}

		events <- event1
		events <- event2
		events <- event3

		close(events)

		blockEvents1 := <-blockEventsCh
		blockEvents2 := <-blockEventsCh
		blockEvents3 := <-blockEventsCh

		require.Equal(t, uint64(1), blockEvents1.BlockNumber)
		require.Equal(t, []ethtypes.Log{event1}, blockEvents1.Events)

		require.Equal(t, uint64(2), blockEvents2.BlockNumber)
		require.Equal(t, []ethtypes.Log{event2}, blockEvents2.Events)

		require.Equal(t, uint64(3), blockEvents3.BlockNumber)
		require.Equal(t, []ethtypes.Log{event3}, blockEvents3.Events)

		_, more := <-blockEventsCh
		require.False(t, more)
	})

	t.Run("EventsWithBlockNumberZero", func(t *testing.T) {
		eb := eventbatcher.NewEventBatcher()

		events := make(chan ethtypes.Log)

		blockEventsCh := eb.BatchEvents(events)

		event1 := ethtypes.Log{
			BlockNumber: 0,
			Address:     common.Address{},
			Topics:      []common.Hash{},
			Data:        []byte{},
		}
		event2 := ethtypes.Log{
			BlockNumber: 0,
			Address:     common.Address{},
			Topics:      []common.Hash{},
			Data:        []byte{},
		}

		events <- event1
		events <- event2

		close(events)

		blockEvents := <-blockEventsCh

		require.Equal(t, uint64(0), blockEvents.BlockNumber)
		require.Equal(t, []ethtypes.Log{event1, event2}, blockEvents.Events)

		_, more := <-blockEventsCh
		require.False(t, more)
	})

	t.Run("ClosedEventsChannel", func(t *testing.T) {
		eb := eventbatcher.NewEventBatcher()

		events := make(chan ethtypes.Log)
		close(events)

		blockEventsCh := eb.BatchEvents(events)

		_, more := <-blockEventsCh
		require.False(t, more)
	})
}
