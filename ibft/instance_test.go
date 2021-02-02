package ibft

import (
	"testing"
	"time"

	"github.com/bloxapp/ssv/ibft/valparser/weekday"

	"github.com/bloxapp/ssv/network/local"

	"github.com/bloxapp/ssv/ibft/proto"

	"github.com/herumi/bls-eth-go-binary/bls"
	"go.uber.org/zap/zaptest"
)

func generateNodes(cnt int) (map[uint64]*bls.SecretKey, map[uint64]*proto.Node) {
	bls.Init(bls.BLS12_381)
	nodes := make(map[uint64]*proto.Node)
	sks := make(map[uint64]*bls.SecretKey)
	for i := 0; i < cnt; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		nodes[uint64(i)] = &proto.Node{
			IbftId: uint64(i),
			Pk:     sk.GetPublicKey().Serialize(),
		}
		sks[uint64(i)] = sk
	}
	return sks, nodes
}

func TestIBFTInstance_Start(t *testing.T) {
	logger := zaptest.NewLogger(t)
	instances := make([]*Instance, 0)
	sks, nodes := generateNodes(4)
	replay := local.NewIBFTReplay(nodes)
	params := &proto.InstanceParams{
		ConsensusParams: proto.DefaultConsensusParams(),
		IbftCommittee:   nodes,
	}

	// setup scenario
	//replay.StartRound(1).PreventMessages(proto.RoundState_Prepare, []uint64{0, 1}).EndRound()

	for i := 0; i < params.CommitteeSize(); i++ {
		me := &proto.Node{
			IbftId: uint64(i),
			Pk:     nodes[uint64(i)].Pk,
			Sk:     sks[uint64(i)].Serialize(),
		}
		instances = append(instances, NewInstance(InstanceOptions{
			Logger:    logger,
			Me:        me,
			Network:   replay.Network,
			Consensus: weekday.New(),
			Params:    params,
		}))
		instances[i].StartEventLoop()
		instances[i].StartMessagePipeline()
	}

	for _, i := range instances {
		go i.Start([]byte{}, []byte("0"), []byte(time.Now().Weekday().String()))
	}

	time.Sleep(time.Minute * 2)
}
