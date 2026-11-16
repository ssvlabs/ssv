package topics

import (
	"crypto/rand"
	"encoding/hex"
	"testing"

	ps_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
)

func randomPeerID(t testing.TB) []byte {
	id := make([]byte, 34)
	id[0], id[1] = 0x00, 32 // identity multihash with a 32-byte digest
	_, _ = rand.Read(id[2:])
	_, err := peer.IDFromBytes(id)
	require.NoError(t, err)
	return id
}

// the msg_id is the same for any two valid From values.
func TestMsgIDIgnoresFrom(t *testing.T) {
	data := []byte("payload")
	require.Equal(t,
		PubsubMsgID(&ps_pb.Message{Data: data, From: randomPeerID(t)}),
		PubsubMsgID(&ps_pb.Message{Data: data, From: randomPeerID(t)}),
	)
}

// a well-formed message has a stable msg_id. Topic and Seqno are set because the id
// does not depend on them.
// The golden value was computed on 52a63c02.
func TestMsgIDGolden(t *testing.T) {
	pid, err := peer.Decode("12D3KooWJ7Y9Yx2ZpSiCxGWmBJXkQ1pDD5BqzLhSzrzmF8Es5Jtm")
	require.NoError(t, err)
	topic := "ssv.v2.42"
	got := PubsubMsgID(&ps_pb.Message{
		Data:  []byte("ssv msg-id golden payload"),
		From:  []byte(pid),
		Seqno: []byte{1, 2, 3, 4, 5, 6, 7, 8},
		Topic: &topic,
	})
	require.Equal(t, "a38a671605aade2900000000", hex.EncodeToString([]byte(got)))
}

// empty messages keep their sentinel msg_id.
func TestMsgIDEmpty(t *testing.T) {
	require.Equal(t, MsgIDEmptyMessage, PubsubMsgID(nil))
	require.Equal(t, MsgIDEmptyMessage, PubsubMsgID(&ps_pb.Message{}))
	require.Equal(t, MsgIDEmptyMessage, PubsubMsgID(&ps_pb.Message{Data: []byte{}, From: randomPeerID(t)}))
}

// a missing or malformed From gets the shared msg_id, so all nodes agree on it.
func TestMsgIDBadFrom(t *testing.T) {
	data := []byte("payload")
	require.Equal(t, "invalid:peer_id_error", MsgIDBadPeerID)
	require.Equal(t, MsgIDBadPeerID, PubsubMsgID(&ps_pb.Message{Data: data}))
	require.Equal(t, MsgIDBadPeerID, PubsubMsgID(&ps_pb.Message{Data: data, From: []byte("not a peer id")}))
	require.NotEqual(t, MsgIDBadPeerID, PubsubMsgID(&ps_pb.Message{Data: data, From: randomPeerID(t)}))
}

// 60,000 copies of one payload with distinct From values.
// No assertion: wall-clock limits flake in CI.
func BenchmarkMsgIDVaryingFrom(b *testing.B) {
	data := make([]byte, 1024)
	_, _ = rand.Read(data)
	msgs := make([]*ps_pb.Message, 60000)
	for i := range msgs {
		msgs[i] = &ps_pb.Message{Data: data, From: randomPeerID(b)}
	}
	want := PubsubMsgID(msgs[0])
	b.ResetTimer()
	for range b.N {
		for _, m := range msgs {
			if PubsubMsgID(m) != want {
				b.Fatal("msg_id depends on From")
			}
		}
	}
}
