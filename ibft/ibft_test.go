package ibft

import (
	"testing"
	"time"

	"github.com/herumi/bls-eth-go-binary/bls"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/ibft/implementations/day_number_consensus"
	"github.com/bloxapp/ssv/ibft/types"
)

func generateNodes(cnt int) (map[uint64]*bls.SecretKey, map[uint64]*types.Node) {
	bls.Init(bls.BLS12_381)
	nodes := make(map[uint64]*types.Node)
	sks := make(map[uint64]*bls.SecretKey)
	for i := 0; i < cnt; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		nodes[uint64(i)] = &types.Node{
			IbftId: uint64(i),
			Pk:     sk.GetPublicKey().Serialize(),
		}
		sks[uint64(i)] = sk
	}
	return sks, nodes
}

func TestIBFTInstance_Start(t *testing.T) {
	instances := make([]*Instance, 0)
	sks, nodes := generateNodes(4)
	replay := NewIBFTReplay(nodes)
	params := &types.InstanceParams{
		ConsensusParams: types.DefaultConsensusParams(),
		IbftCommittee:   nodes,
	}

	// setup scenario
	replay.StartRound(1).PreventMessages(types.RoundState_Commit, []uint64{0, 1}).EndRound()

	leader := params.CommitteeSize() - 1
	for i := 0; i < params.CommitteeSize(); i++ {
		me := &types.Node{
			IbftId: uint64(i),
			Pk:     nodes[uint64(i)].Pk,
			Sk:     sks[uint64(i)].Serialize(),
		}
		instances = append(instances, New(me, replay.Networker, &day_number_consensus.DayNumberConsensus{Id: uint64(i), Leader: uint64(leader)}, params))
		instances[i].StartEventLoopAndMessagePipeline()
	}

	for _, i := range instances {
		require.NoError(t, i.Start([]byte("0"), []byte(time.Now().Weekday().String())))
	}

	time.Sleep(time.Minute * 5)
}
