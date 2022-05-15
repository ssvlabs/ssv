package instance

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/leader/deterministic"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont/inmem"
)

func TestLeaderCalculation(t *testing.T) {
	// leader selection
	l, err := deterministic.New(append([]byte{1, 2, 3, 2, 5, 6, 1, 1}, []byte(strconv.FormatUint(1, 10))...), 4)
	require.NoError(t, err)

	round := atomic.Value{}
	round.Store(message.Round(1))
	preparedRound := atomic.Value{}
	preparedRound.Store(message.Round(0))
	preparedValue := atomic.Value{}
	preparedValue.Store([]byte(nil))
	identifier := atomic.Value{}
	identifier.Store([]byte(nil))

	sk, nodes := GenerateNodes(4)
	instance := &Instance{
		PrepareMessages: inmem.New(3, 2),
		Config:          qbft.DefaultConsensusParams(),
		ValidatorShare:  &beacon.Share{Committee: nodes, NodeID: 1, PublicKey: sk[1].GetPublicKey()},
		state: &qbft.State{
			Round:         round,
			PreparedRound: preparedRound,
			PreparedValue: preparedValue,
			Identifier:    identifier,
		},
		LeaderSelector: l,
	}

	for i := 1; i < 50; i++ {
		t.Run(fmt.Sprintf("round %d", i), func(t *testing.T) {
			instance.BumpRound()
			require.EqualValues(t, uint64(i%4+1), instance.ThisRoundLeader())
			require.EqualValues(t, uint64(i+1), instance.State().Round.Load())

			if instance.ThisRoundLeader() == 1 {
				require.True(t, instance.IsLeader())
			} else {
				require.False(t, instance.IsLeader())
			}
		})
	}
}
