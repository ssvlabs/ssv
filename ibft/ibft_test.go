package ibft

import (
	"encoding/binary"
	"github.com/bloxapp/ssv/storage/inmem"
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
	network := local.NewLocalNetwork()
	storage := inmem.New()

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

		ibft := New(storage, me, network, params)
		instances = append(instances, ibft)
	}

	// start repeated timer
	ticker := time.NewTicker(11 * time.Second)
	quit := make(chan struct{})
	instanceIdentifier := FirstInstanceIdentifier()
	instanceCount := 0
	go func() {
		for {
			select {
			case <-ticker.C:
				newID := Int64ToBytes(time.Now().Unix())
				opts := StartOptions{
					Logger:       logger,
					Consensus:    bytesval.New([]byte(time.Now().Weekday().String())),
					PrevInstance: instanceIdentifier,
					Identifier:   newID,
					Value:        []byte(time.Now().Weekday().String()),
				}

				/**
				replay config
				*/
				//if instanceCount == 0 {
				//	s := local.NewRoundScript(replay, []uint64{0, 1, 2, 3})
				//	s.PreventMessages(proto.RoundState_PrePrepare, []uint64{0})
				//	replay.SetScript(newID, 1, s)
				//}

				//s1 := local.NewRoundScript(replay, []uint64{0, 1, 2, 3})
				//s1.PreventMessages(proto.RoundState_PrePrepare, []uint64{1})
				//replay.SetScript(2, s1)
				/**
				replay config
				*/

				opts.Logger.Info("\n\n\nStarting new instance\n\n\n", zap.Binary("id", newID), zap.Binary("prev_id", instanceIdentifier))
				for _, i := range instances {
					go i.StartInstance(opts)
				}
				instanceCount++
				//instanceIdentifier = newID
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()

	// wait
	time.Sleep(time.Minute * 2)
}

func Int64ToBytes(i int64) []byte {
	var buf = make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, uint64(i))
	return buf
}
