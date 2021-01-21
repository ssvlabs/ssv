package ibft

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/ibft/implementations/day_number_consensus"

	"github.com/bloxapp/ssv/ibft/types"
)

type Local4NodeNetworker struct {
	c []chan *types.Message
	l []sync.Mutex
}

func (n *Local4NodeNetworker) ReceivedMsgChan() chan *types.Message {
	c := make(chan *types.Message)
	l := sync.Mutex{}
	n.c = append(n.c, c)
	n.l = append(n.l, l)
	return c
}

func (n *Local4NodeNetworker) Broadcast(msg *types.Message) error {
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
	net := &Local4NodeNetworker{c: make([]chan *types.Message, 0)}
	nodes := []*iBFTInstance{
		New(3, net, &day_number_consensus.DayNumberConsensus{Id: 3, Leader: 0}, types.BasicParams),
		New(2, net, &day_number_consensus.DayNumberConsensus{Id: 2, Leader: 0}, types.BasicParams),
		New(1, net, &day_number_consensus.DayNumberConsensus{Id: 1, Leader: 0}, types.BasicParams),
		New(0, net, &day_number_consensus.DayNumberConsensus{Id: 0, Leader: 0}, types.BasicParams),
	}

	for _, i := range nodes {
		i.StartEventLoop()
	}

	for _, i := range nodes {
		require.NoError(t, i.Start([]byte("0"), []byte(time.Now().Weekday().String())))
	}

	<-nodes[0].Committed()
}
