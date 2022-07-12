package instance

import (
	"context"
	"fmt"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/roundtimer"
	"go.uber.org/zap"
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

	ctx := context.Background()
	sk, nodes := GenerateNodes(4)
	instance := &Instance{
		Logger:          zap.L(),
		ctx:             ctx,
		PrepareMessages: inmem.New(3, 2),
		Config:          qbft.DefaultConsensusParams(),
		ValidatorShare:  &beacon.Share{Committee: nodes, NodeID: 1, PublicKey: sk[1].GetPublicKey()},
		state: &qbft.State{
			Round: round,
		},
		LeaderSelector: l,
		roundTimer:     roundtimer.New(ctx, zap.L()),
	}

	for i := 1; i < 50; i++ {
		t.Run(fmt.Sprintf("round %d", i), func(t *testing.T) {
			instance.BumpRound()
			require.EqualValues(t, uint64(i%4+1), instance.ThisRoundLeader())
			require.EqualValues(t, uint64(i+1), instance.State().GetRound())

			if instance.ThisRoundLeader() == 1 {
				require.True(t, instance.IsLeader())
			} else {
				require.False(t, instance.IsLeader())
			}
		})
	}
}
