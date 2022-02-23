package local

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

func TestMsgChan(t *testing.T) {
	net := NewLocalNetwork()
	c1, _ := net.ReceivedMsgChan()
	c2, _ := net.ReceivedMsgChan()
	c3, _ := net.ReceivedMsgChan()
	c4, _ := net.ReceivedMsgChan()

	testMsg := &proto.SignedMessage{
		Signature: []byte{1, 2, 3, 4},
		SignerIds: []uint64{1, 2, 3, 4},
	}

	lock := sync.Mutex{}
	doneFlags := []bool{false, false, false, false}
	for i, c := range []<-chan *proto.SignedMessage{c1, c2, c3, c4} {
		go func(c <-chan *proto.SignedMessage, flag *bool) {
			msg := <-c
			lock.Lock()
			defer lock.Unlock()
			require.EqualValues(t, []byte{1, 2, 3, 4}, msg.Signature)
			require.EqualValues(t, []uint64{1, 2, 3, 4}, msg.SignerIds)
			*flag = true
		}(c, &doneFlags[i])
	}

	time.Sleep(time.Millisecond * 100)
	net.Broadcast([]byte{1}, testMsg)
	time.Sleep(time.Millisecond * 500)

	lock.Lock()
	defer lock.Unlock()
	require.EqualValues(t, []bool{true, true, true, true}, doneFlags)
}

func TestSigChan(t *testing.T) {
	net := NewLocalNetwork()
	c1, _ := net.ReceivedSignatureChan()
	c2, _ := net.ReceivedSignatureChan()
	c3, _ := net.ReceivedSignatureChan()
	c4, _ := net.ReceivedSignatureChan()
	testMsg := &proto.SignedMessage{
		Signature: []byte{1, 2, 3, 4},
		SignerIds: []uint64{1, 2, 3, 4},
	}

	lock := sync.Mutex{}
	doneFlags := []bool{false, false, false, false}
	for i, c := range []<-chan *proto.SignedMessage{c1, c2, c3, c4} {
		go func(c <-chan *proto.SignedMessage, flag *bool) {
			msg := <-c
			lock.Lock()
			defer lock.Unlock()
			require.EqualValues(t, []byte{1, 2, 3, 4}, msg.Signature)
			require.EqualValues(t, []uint64{1, 2, 3, 4}, msg.SignerIds)
			*flag = true
		}(c, &doneFlags[i])
	}

	time.Sleep(time.Millisecond * 100)
	net.BroadcastSignature([]byte{1}, testMsg)
	time.Sleep(time.Millisecond * 100)

	lock.Lock()
	defer lock.Unlock()
	require.EqualValues(t, []bool{true, true, true, true}, doneFlags)
}

func TestDecidedChan(t *testing.T) {
	net := NewLocalNetwork()
	c1, _ := net.ReceivedDecidedChan()
	c2, _ := net.ReceivedDecidedChan()
	c3, _ := net.ReceivedDecidedChan()
	c4, _ := net.ReceivedDecidedChan()

	testMsg := &proto.SignedMessage{
		Signature: []byte{1, 2, 3, 4},
		SignerIds: []uint64{1, 2, 3, 4},
	}

	lock := sync.Mutex{}
	doneFlags := []bool{false, false, false, false}
	for i, c := range []<-chan *proto.SignedMessage{c1, c2, c3, c4} {
		go func(c <-chan *proto.SignedMessage, flag *bool) {
			msg := <-c
			lock.Lock()
			defer lock.Unlock()
			require.EqualValues(t, []byte{1, 2, 3, 4}, msg.Signature)
			require.EqualValues(t, []uint64{1, 2, 3, 4}, msg.SignerIds)
			*flag = true
		}(c, &doneFlags[i])
	}

	time.Sleep(time.Millisecond * 100)
	net.BroadcastDecided([]byte{1}, testMsg)
	time.Sleep(time.Millisecond * 100)

	lock.Lock()
	defer lock.Unlock()
	require.EqualValues(t, []bool{true, true, true, true}, doneFlags)
}

func TestGetHighestDecided(t *testing.T) {
	net := NewLocalNetwork()
	_, _ = net.ReceivedSyncMsgChan()
	c2, _ := net.ReceivedSyncMsgChan()

	go func() {
		msg := <-c2
		require.EqualValues(t, []byte{1, 2, 3, 4}, msg.Msg.Lambda)
		require.EqualValues(t, network.Sync_GetHighestType, msg.Msg.Type)
		require.NoError(t, net.RespondSyncMsg(msg.StreamID, &network.SyncMessage{
			Lambda:     []byte{1, 1, 1, 1},
			FromPeerID: "1",
			Type:       network.Sync_GetHighestType,
		}))
	}()

	res, err := net.GetHighestDecidedInstance("1", &network.SyncMessage{
		Lambda:     []byte{1, 2, 3, 4},
		FromPeerID: "0",
		Type:       network.Sync_GetHighestType,
	})
	require.NoError(t, err)
	require.EqualValues(t, []byte{1, 1, 1, 1}, res.Lambda)
}

func TestGetDecidedByRange(t *testing.T) {
	net := NewLocalNetwork()
	c, _ := net.ReceivedSyncMsgChan()

	go func() {
		msg := <-c
		require.EqualValues(t, []byte{1, 2, 3, 4}, msg.Msg.Lambda)
		require.EqualValues(t, network.Sync_GetHighestType, msg.Msg.Type)
		require.NoError(t, net.RespondSyncMsg(msg.StreamID, &network.SyncMessage{
			Lambda:     []byte{1, 1, 1, 1},
			FromPeerID: "1",
			Type:       network.Sync_GetHighestType,
		}))
	}()

	res, err := net.GetDecidedByRange("0", &network.SyncMessage{
		Lambda:     []byte{1, 2, 3, 4},
		FromPeerID: "0",
		Type:       network.Sync_GetHighestType,
	})
	require.NoError(t, err)
	require.EqualValues(t, []byte{1, 1, 1, 1}, res.Lambda)
}

func TestGetAllPeers(t *testing.T) {
	net := NewLocalNetwork()
	_, _ = net.ReceivedSyncMsgChan()
	_, _ = net.ReceivedSyncMsgChan()
	_, _ = net.ReceivedSyncMsgChan()
	_, _ = net.ReceivedSyncMsgChan()

	res, err := net.AllPeers([]byte{})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"0", "1", "2", "3"}, res)
}
