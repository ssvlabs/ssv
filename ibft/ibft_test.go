package ibft

import (
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/val/weekday"

	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network/local"
	"go.uber.org/zap/zaptest"
)

func TestIBFT(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sks, nodes := generateNodes(4)
	replay := local.NewIBFTReplay(nodes)
	params := &proto.InstanceParams{
		ConsensusParams: proto.DefaultConsensusParams(),
		IbftCommittee:   nodes,
	}
	instances := make([]*IBFT, 0)

	for i := 0; i < params.CommitteeSize(); i++ {
		me := &proto.Node{
			IbftId: uint64(i),
			Pk:     nodes[uint64(i)].Pk,
			Sk:     sks[uint64(i)].Serialize(),
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
	ticker := time.NewTicker(10 * time.Second)
	quit := make(chan struct{})
	identfier := FirstInstanceIdentifier
	go func() {
		for {
			select {
			case <-ticker.C:
				newId := time.Now().String()
				opts := StartOptions{
					Logger:       logger,
					Consensus:    weekday.New(),
					PrevInstance: []byte(identfier),
					Identifier:   []byte(newId),
					Value:        []byte(time.Now().Weekday().String()),
				}
				replay := local.NewIBFTReplay(nodes)
				opts.Logger.Info("\n\n\nStarting new instance\n\n\n", zap.String("id", newId), zap.String("prev_id", identfier))
				for _, i := range instances {
					i.network = replay.Network
					go i.StartInstance(opts)
				}
				identfier = newId
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()

	// wait
	time.Sleep(time.Minute * 2)
}
