package instance
//
//import (
//	"fmt"
//	msgcontinmem "github.com/bloxapp/ssv/ibft/instance/msgcont/inmem"
//	"github.com/bloxapp/ssv/ibft/leader/deterministic"
//	"github.com/bloxapp/ssv/ibft/proto"
//	"github.com/bloxapp/ssv/protocol/v1/keymanager"
//	"github.com/bloxapp/ssv/protocol/v1/message"
//	"github.com/bloxapp/ssv/protocol/v1/qbft"
//	"github.com/stretchr/testify/require"
//	"go.uber.org/atomic"
//	"strconv"
//	"testing"
//)
//
//func TestLeaderCalculation(t *testing.T) {
//	// leader selection
//	l, err := deterministic.New(append([]byte{1, 2, 3, 2, 5, 6, 1, 1}, []byte(strconv.FormatUint(1, 10))...), 4)
//	require.NoError(t, err)
//
//	round := atomic.Value{}
//	round.Store(message.Round(1))
//	preparedRound := atomic.Uint64{}
//	preparedRound.Store(0)
//
//	sk, nodes := GenerateNodes(4)
//	instance := &Instance{
//		PrepareMessages: msgcontinmem.New(3, 2),
//		Config:          proto.DefaultConsensusParams(),
//		ValidatorShare:  &keymanager.Share{Committee: nodes, NodeID: 1, PublicKey: sk[1].GetPublicKey()},
//		state: &qbft.State{
//			Round:         round,
//			PreparedRound: preparedRound,
//			PreparedValue: atomic.String{},
//			Identifier:    atomic.String{},
//		},
//		LeaderSelector: l,
//	}
//
//	for i := 1; i < 50; i++ {
//		t.Run(fmt.Sprintf("round %d", i), func(t *testing.T) {
//			instance.BumpRound()
//			require.EqualValues(t, uint64(i%4+1), instance.ThisRoundLeader())
//			require.EqualValues(t, uint64(i+1), instance.State().Round.Get())
//
//			if instance.ThisRoundLeader() == 1 {
//				require.True(t, instance.IsLeader())
//			} else {
//				require.False(t, instance.IsLeader())
//			}
//		})
//	}
//}
