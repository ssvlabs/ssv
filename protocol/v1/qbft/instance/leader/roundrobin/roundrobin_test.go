package roundrobin

import (
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	qbftprotocol "github.com/bloxapp/ssv/protocol/v1/qbft"
)

func TestRoundRobin_Calculate(t *testing.T) {
	tests := []struct {
		name     string
		share    *beaconprotocol.Share
		state    *qbftprotocol.State
		round    uint64
		expected uint64
		panics   bool
	}{
		{
			name:     "Round = 1, height = 0",
			share:    newShare(),
			state:    stateWithHeight(0),
			round:    1,
			expected: 1,
		},
		{
			name:     "Round = 1, height = 1",
			share:    newShare(),
			state:    stateWithHeight(1),
			round:    1,
			expected: 2,
		},
		{
			name:     "Round = 7, height = 9",
			share:    newShare(),
			state:    stateWithHeight(9),
			round:    7,
			expected: 4,
		},
		{
			name:   "Round = 0, panic",
			share:  newShare(),
			state:  stateWithHeight(0),
			round:  0,
			panics: true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			rr := New(tt.share, tt.state)
			if tt.panics {
				panicFunc := func() {
					rr.Calculate(tt.round)
				}

				require.Panics(t, panicFunc)
			} else {
				require.Equal(t, tt.expected, rr.Calculate(tt.round))
			}
		})
	}
}

func newShare() *beaconprotocol.Share {
	return &beaconprotocol.Share{
		Committee:   committeeMap(),
		OperatorIds: operatorIDs(),
	}
}

func operatorIDs() []uint64 {
	return []uint64{1, 2, 3, 4}
}

func committeeMap() map[spectypes.OperatorID]*beaconprotocol.Node {
	ids := operatorIDs()
	result := make(map[spectypes.OperatorID]*beaconprotocol.Node)
	for _, id := range ids {
		result[spectypes.OperatorID(id)] = &beaconprotocol.Node{
			IbftID: id,
		}
	}

	return result
}

func stateWithHeight(height specqbft.Height) *qbftprotocol.State {
	var state qbftprotocol.State
	state.Height.Store(height)

	return &state
}
