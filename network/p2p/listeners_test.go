package p2p

import (
	"context"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"go.uber.org/zap/zaptest"
	"sync"
	"testing"
)

func TestListeners(t *testing.T) {
	n := p2pNetwork{
		logger:        zaptest.NewLogger(t),
		listeners:     map[string]listener{},
		listenersLock: &sync.Mutex{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	ibftCn1, ibftCnDone1 := n.ReceivedMsgChan()
	// checks that ibftCn1 received 3 messages
	wg.Add(3)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-ibftCn1:
				if ok {
					wg.Done()
				}
			}
		}
	}()

	ibftCn2, ibftCnDone2 := n.ReceivedMsgChan()
	// checks that ibftCn2 received 2 messages (it will be closed after 2 messages)
	wg.Add(2)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-ibftCn2:
				if ok {
					wg.Done()
				}
			}
		}
	}()

	msg := &proto.SignedMessage{
		Message: &proto.Message{
			Type:      proto.RoundState_ChangeRound,
			Round:     1,
			Lambda:    []byte{1, 2, 3, 4},
			SeqNumber: 1,
			Value:     []byte{},
		},
		Signature: []byte{},
		SignerIds: []uint64{1},
	}

	n.propagateSignedMsg(&network.Message{Type: network.NetworkMsg_IBFTType, SignedMessage: msg})
	n.propagateSignedMsg(&network.Message{Type: network.NetworkMsg_IBFTType, SignedMessage: msg})
	n.propagateSignedMsg(&network.Message{Type: network.NetworkMsg_DecidedType, SignedMessage: msg})
	ibftCnDone2()
	n.propagateSignedMsg(&network.Message{Type: network.NetworkMsg_IBFTType, SignedMessage: msg})
	ibftCnDone1()

	wg.Wait()
}
