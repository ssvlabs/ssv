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
	c []chan *types.Message
	l []sync.Mutex
}

func (n *LocalNodeNetworker) ReceivedMsgChan() chan *types.Message {
	c := make(chan *types.Message)
	l := sync.Mutex{}
	n.c = append(n.c, c)
	n.l = append(n.l, l)
	return c
}

func (n *LocalNodeNetworker) Broadcast(msg *types.Message) error {
	go func() {
		for i, c := range n.c {
			n.l[i].Lock()
			c <- msg
			n.l[i].Unlock()
		}
	}()

	return nil
}

func generateNodes(cnt int) []*types.Node {
	bls.Init(bls.BLS12_381)
	ret := make([]*types.Node, cnt)
	for i := 0; i < cnt; i++ {
		sk := bls.SecretKey{}
		sk.SetByCSPRNG()

		ret[i] = &types.Node{
			IbftId: uint64(i),
			Pk:     sk.GetPublicKey().Serialize(),
		}
	}
	return ret
}

func TestIBFTInstance_Start(t *testing.T) {
	net := &LocalNodeNetworker{c: make([]chan *types.Message, 0), l: make([]sync.Mutex, 0)}
	instances := make([]*iBFTInstance, 0)
	nodes := generateNodes(4)
	params := &types.InstanceParams{
		ConsensusParams: types.DefaultConsensusParams(),
		IbftCommittee:   nodes,
	}

	leader := params.CommitteeSize() - 1
	for i := 0; i < params.CommitteeSize(); i++ {
		instances = append(instances, New(nodes[i], net, &day_number_consensus.DayNumberConsensus{Id: uint64(i), Leader: uint64(leader)}, params))
		instances[i].StartEventLoop()
	}

	for _, i := range instances {
		require.NoError(t, i.Start([]byte("0"), []byte(time.Now().Weekday().String())))
	}

	time.Sleep(time.Second * 5)
}
