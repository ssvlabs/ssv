package ibft

import (
	"testing"
	"time"

	"go.uber.org/zap/zaptest"

	"github.com/bloxapp/ssv/ibft/proto"
	ibfttesting "github.com/bloxapp/ssv/ibft/spectesting"
	"github.com/bloxapp/ssv/network/local"
	"github.com/bloxapp/ssv/utils/dataval/bytesval"
)

func TestInstance_Start(t *testing.T) {
	logger := zaptest.NewLogger(t)
	instances := make([]*Instance, 0)
	secretKeys, nodes := ibfttesting.GenerateNodes(4)
	network := local.NewLocalNetwork()
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
			Sk:     secretKeys[uint64(i)].Serialize(),
		}
		instances = append(instances, NewInstance(InstanceOptions{
			Logger:         logger,
			Me:             me,
			Network:        network,
			Consensus:      bytesval.New([]byte(time.Now().Weekday().String())),
			Params:         params,
			Lambda:         []byte("0"),
			PreviousLambda: []byte(""),
		}))
		go instances[i].StartEventLoop()
		go instances[i].StartMessagePipeline()
	}

	for _, i := range instances {
		go i.Start([]byte(time.Now().Weekday().String()))
	}

	time.Sleep(time.Minute * 2)
}
