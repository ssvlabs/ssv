package instance

import (
	"fmt"
	"strconv"
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"

	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/leader/deterministic"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont/inmem"
)

func TestLeaderCalculation(t *testing.T) {
	// leader selection
	l, err := deterministic.New(append([]byte{1, 2, 3, 2, 5, 6, 1, 1}, []byte(strconv.FormatUint(1, 10))...), 4)
	require.NoError(t, err)

	round := atomic.Value{}
	round.Store(specqbft.Round(1))

	sk, nodes, operatorIds, shareOperatorIds := GenerateNodes(4)

	instance := &Instance{
		containersMap: map[specqbft.MessageType]msgcont.MessageContainer{
			specqbft.PrepareMsgType: inmem.New(3, 2),
		},
		Config: qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{
			Committee:   nodes,
			NodeID:      operatorIds[0],
			PublicKey:   sk[operatorIds[0]].GetPublicKey(),
			OperatorIds: shareOperatorIds,
		},
		state: &qbft.State{
			Round: round,
		},
		LeaderSelector: l,
	}

	for i := 1; i < 50; i++ {
		t.Run(fmt.Sprintf("round %d", i), func(t *testing.T) {
			instance.BumpRound()
			require.EqualValues(t, operatorIds[i%4], instance.ThisRoundLeader())
			require.EqualValues(t, uint64(i+1), instance.State().GetRound())

			if instance.ThisRoundLeader() == uint64(operatorIds[0]) {
				require.True(t, instance.IsLeader())
			} else {
				require.False(t, instance.IsLeader())
			}
		})
	}
}
