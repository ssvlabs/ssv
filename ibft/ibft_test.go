package ibft

import (
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/bloxapp/ssv/ibft/proto"
	ibfttesting "github.com/bloxapp/ssv/ibft/spec_testing"
	"github.com/bloxapp/ssv/network/local"
	"github.com/bloxapp/ssv/utils/dataval/bytesval"
)

func TestIBFT(t *testing.T) {
	logger := zaptest.NewLogger(t)
	secretKeys, nodes := ibfttesting.GenerateNodes(4)
	replay := local.NewIBFTReplay(nodes)
	params := &proto.InstanceParams{
		ConsensusParams: proto.DefaultConsensusParams(),
		IbftCommittee:   nodes,
	}
	instances := make([]IBFT, 0)

	for i := 0; i < params.CommitteeSize(); i++ {
		me := &proto.Node{
			IbftId: uint64(i),
			Pk:     nodes[uint64(i)].Pk,
			Sk:     secretKeys[uint64(i)].Serialize(),
		}

		ibft := New(replay.Storage, me, replay.Network, params)
		instances = append(instances, ibft)

		//instances = append(instances, NewInstance(InstanceOptions{
		//	Logger:    logger,
		//	Me:        me,
		//	Network:   replay.Network,
		//	Consensus: weekday.New(),
		//	Params:    params,
		//}))
		//instances[i].StartEventLoop()
		//instances[i].StartMessagePipeline()
	}

	// start repeated timer
	ticker := time.NewTicker(20 * time.Second)
	quit := make(chan struct{})
	instanceIdentifier := FirstInstanceIdentifier
	go func() {
		for {
			select {
			case <-ticker.C:
				newID := time.Now().String()
				opts := StartOptions{
					Logger:       logger,
					Consensus:    bytesval.New([]byte(time.Now().Weekday().String())),
					PrevInstance: []byte(instanceIdentifier),
					Identifier:   []byte(newID),
					Value:        []byte(time.Now().Weekday().String()),
				}
				replay := local.NewIBFTReplay(nodes)
				opts.Logger.Info("\n\n\nStarting new instance\n\n\n", zap.String("id", newID), zap.String("prev_id", instanceIdentifier))
				for _, i := range instances {
					i.(*ibftImpl).network = replay.Network
					go i.StartInstance(opts)
				}
				instanceIdentifier = newID
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()

	// wait
	time.Sleep(time.Minute * 2)
}
