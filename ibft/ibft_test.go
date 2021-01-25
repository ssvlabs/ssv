package ibft

import (
	"sync"
	"testing"
	"time"

	"github.com/herumi/bls-eth-go-binary/bls"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/ibft/implementations/day_number_consensus"
	"github.com/bloxapp/ssv/ibft/types"
)

type LocalNodeNetworker struct {
	c []chan *types.SignedMessage
	l []sync.Mutex
}

func (n *LocalNodeNetworker) ReceivedMsgChan() chan *types.SignedMessage {
	c := make(chan *types.SignedMessage)
	l := sync.Mutex{}
	n.c = append(n.c, c)
	n.l = append(n.l, l)
	return c
}

func (n *LocalNodeNetworker) Broadcast(msg *types.Message) error {
	go func() {
		for i, c := range n.c {
			n.l[i].Lock()
			c <- &types.SignedMessage{ // TODO - broadcast only signed messages
				Message:   msg,
				Signature: nil,
				IbftId:    0,
			}
			n.l[i].Unlock()
		}
	}()

	return nil
}

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
	net := &LocalNodeNetworker{c: make([]chan *types.SignedMessage, 0), l: make([]sync.Mutex, 0)}
	instances := make([]*iBFTInstance, 0)
	_, nodes := generateNodes(4)
	params := &types.InstanceParams{
		ConsensusParams: types.DefaultConsensusParams(),
		IbftCommittee:   nodes,
	}

	leader := params.CommitteeSize() - 1
	for i := 0; i < params.CommitteeSize(); i++ {
		instances = append(instances, New(nodes[uint64(i)], net, &day_number_consensus.DayNumberConsensus{Id: uint64(i), Leader: uint64(leader)}, params))
		instances[i].StartEventLoop()
	}

	for _, i := range instances {
		require.NoError(t, i.Start([]byte("0"), []byte(time.Now().Weekday().String())))
	}

	time.Sleep(time.Second * 5)
}
