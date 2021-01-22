package ibft

import (
	"sync"
	"testing"
	"time"

	"github.com/bloxapp/ssv/ibft/implementations/day_number_consensus"
	"github.com/stretchr/testify/require"

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

func TestIBFTInstance_Start(t *testing.T) {
	net := &LocalNodeNetworker{c: make([]chan *types.Message, 0), l: make([]sync.Mutex, 0)}
	nodes := make([]*iBFTInstance, 0)

	leader := types.BasicParams.IbftCommitteeSize - 1
	for i := uint64(0); i < types.BasicParams.IbftCommitteeSize; i++ {
		nodes = append(nodes, New(i, net, &day_number_consensus.DayNumberConsensus{Id: i, Leader: leader}, types.BasicParams))
		nodes[i].StartEventLoop()
	}

	for _, i := range nodes {
		require.NoError(t, i.Start([]byte("0"), []byte(time.Now().Weekday().String())))
	}

	time.Sleep(time.Second * 5)
}
