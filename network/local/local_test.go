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
	c1, c2, c3, c4 := net.ReceivedMsgChan(), net.ReceivedMsgChan(), net.ReceivedMsgChan(), net.ReceivedMsgChan()

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
	net.Broadcast(testMsg)
	time.Sleep(time.Millisecond * 500)

	lock.Lock()
	defer lock.Unlock()
	require.EqualValues(t, []bool{true, true, true, true}, doneFlags)
}

func TestSigChan(t *testing.T) {
	net := NewLocalNetwork()
	c1, c2, c3, c4 := net.ReceivedSignatureChan(), net.ReceivedSignatureChan(), net.ReceivedSignatureChan(), net.ReceivedSignatureChan()

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
	net.BroadcastSignature(testMsg)
	time.Sleep(time.Millisecond * 100)

	lock.Lock()
	defer lock.Unlock()
	require.EqualValues(t, []bool{true, true, true, true}, doneFlags)
}

func TestDecidedChan(t *testing.T) {
	net := NewLocalNetwork()
	c1, c2, c3, c4 := net.ReceivedDecidedChan(), net.ReceivedDecidedChan(), net.ReceivedDecidedChan(), net.ReceivedDecidedChan()

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
	net.BroadcastDecided(testMsg)
	time.Sleep(time.Millisecond * 100)

	lock.Lock()
	defer lock.Unlock()
	require.EqualValues(t, []bool{true, true, true, true}, doneFlags)
}

func TestGetHighestDecided(t *testing.T) {
	net := NewLocalNetwork()
	_, c2 := net.ReceivedSyncMsgChan(), net.ReceivedSyncMsgChan()

	go func() {
		msg := <-c2
		require.EqualValues(t, []byte{1, 2, 3, 4}, msg.Msg.ValidatorPk)
		require.EqualValues(t, network.Sync_GetHighestType, msg.Msg.Type)
		net.RespondToHighestDecidedInstance(msg.Stream, &network.SyncMessage{
			ValidatorPk: []byte{1, 1, 1, 1},
			FromPeerID:  "1",
			Type:        network.Sync_GetHighestType,
		})
	}()

	res, err := net.GetHighestDecidedInstance("1", &network.SyncMessage{
		ValidatorPk: []byte{1, 2, 3, 4},
		FromPeerID:  "0",
		Type:        network.Sync_GetHighestType,
	})
	require.NoError(t, err)
	require.EqualValues(t, []byte{1, 1, 1, 1}, res.ValidatorPk)
}

func TestGetDecidedByRange(t *testing.T) {
	net := NewLocalNetwork()
	_, c2 := net.ReceivedSyncMsgChan(), net.ReceivedSyncMsgChan()

	go func() {
		msg := <-c2
		require.EqualValues(t, []byte{1, 2, 3, 4}, msg.Msg.ValidatorPk)
		require.EqualValues(t, network.Sync_GetHighestType, msg.Msg.Type)
		net.RespondToGetDecidedByRange(msg.Stream, &network.SyncMessage{
			ValidatorPk: []byte{1, 1, 1, 1},
			FromPeerID:  "1",
			Type:        network.Sync_GetHighestType,
		})
	}()

	res, err := net.GetDecidedByRange("1", &network.SyncMessage{
		ValidatorPk: []byte{1, 2, 3, 4},
		FromPeerID:  "0",
		Type:        network.Sync_GetHighestType,
	})
	require.NoError(t, err)
	require.EqualValues(t, []byte{1, 1, 1, 1}, res.ValidatorPk)
}

func TestGetAllPeers(t *testing.T) {
	net := NewLocalNetwork()
	_, _, _, _ = net.ReceivedSyncMsgChan(), net.ReceivedSyncMsgChan(), net.ReceivedSyncMsgChan(), net.ReceivedSyncMsgChan()
	res, err := net.AllPeers([]byte{})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"0", "1", "2", "3"}, res)
}
