package p2p

import (
	"encoding/hex"
	"encoding/json"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"testing"
	"time"
)

// ForkV0 is the genesis version 0 implementation
type testingFork struct {
}

func testFork() *testingFork {
	return &testingFork{}
}

func (v0 *testingFork) ValidatorTopicID(pkByts []byte) string {
	return hex.EncodeToString(pkByts)
}

func (v0 *testingFork) EncodeNetworkMsg(msg *network.Message) ([]byte, error) {
	return json.Marshal(msg)
}

func (v0 *testingFork) DecodeNetworkMsg(data []byte) (*network.Message, error) {
	ret := &network.Message{}
	err := json.Unmarshal(data, ret)
	return ret, err
}

func (v0 *testingFork) SlotTick(slot uint64) {

}

func TestSyncMessageBroadcastingTimeout(t *testing.T) {
	logger := logex.Build("test", zap.DebugLevel, nil)

	peer1, peer2 := testPeers(t, logger)

	// broadcast msg
	messageToBroadcast := &network.SyncMessage{
		SignedMessages: nil,
		Type:           network.Sync_GetHighestType,
	}

	peerID := peer.Encode(peer2.(*p2pNetwork).host.ID())
	res, err := peer1.GetHighestDecidedInstance(peerID, messageToBroadcast)
	require.EqualError(t, err, "could not read sync msg: i/o deadline reached")
	time.Sleep(time.Millisecond * 100)
	require.Nil(t, res)
}

func TestSyncMessageBroadcasting(t *testing.T) {
	logger := logex.Build("test", zapcore.InfoLevel, nil)

	peer1, peer2 := testPeers(t, logger)

	// set receivers
	peer2Chan, done := peer2.ReceivedSyncMsgChan()
	defer done()

	var receivedStream network.SyncStream
	go func() {
		msgFromPeer1 := <-peer2Chan
		require.EqualValues(t, peer1.(*p2pNetwork).host.ID().String(), msgFromPeer1.Msg.FromPeerID)
		require.EqualValues(t, network.Sync_GetHighestType, msgFromPeer1.Msg.Type)

		receivedStream = msgFromPeer1.Stream

		messageToBroadcast := &network.SyncMessage{
			SignedMessages: nil,
			Type:           network.Sync_GetHighestType,
		}
		require.NoError(t, peer2.RespondToHighestDecidedInstance(msgFromPeer1.Stream, messageToBroadcast))
	}()

	// broadcast msg
	messageToBroadcast := &network.SyncMessage{
		SignedMessages: nil,
		Type:           network.Sync_GetHighestType,
	}

	time.Sleep(time.Millisecond * 800) // sleep to let nodes reach each other

	peerID := peer.Encode(peer2.(*p2pNetwork).host.ID())
	res, err := peer1.GetHighestDecidedInstance(peerID, messageToBroadcast)
	require.NoError(t, err)
	time.Sleep(time.Millisecond * 100)

	// verify
	require.NotNil(t, res)
	require.EqualValues(t, peer2.(*p2pNetwork).host.ID().String(), res.FromPeerID)
	require.EqualValues(t, network.Sync_GetHighestType, res.Type)

	// verify stream closed
	require.NotNil(t, receivedStream)
	err = receivedStream.WriteWithTimeout([]byte{1}, time.Second*10)
	require.EqualError(t, err, "writen bytes to sync stream doesnt match input data")
}
