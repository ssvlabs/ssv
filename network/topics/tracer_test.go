package topics

import (
	crand "crypto/rand"
	"encoding/hex"
	"testing"

	ps_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// TestPSTracer_RPCEvents locks the trace format of RPC events — in particular the payload message
// ids, which are what allow reconstructing a message's path across nodes from their traces.
func TestPSTracer_RPCEvents(t *testing.T) {
	priv, _, err := libp2pcrypto.GenerateEd25519Key(crand.Reader)
	require.NoError(t, err)
	pid, err := peer.IDFromPrivateKey(priv)
	require.NoError(t, err)

	topic := "ssv.v2.33"
	msgID1 := []byte{0x01, 0x02}
	msgID2 := []byte{0x03, 0x04}

	fullMeta := &ps_pb.TraceEvent_RPCMeta{
		Messages: []*ps_pb.TraceEvent_MessageMeta{
			{MessageID: msgID1, Topic: &topic},
			{MessageID: msgID2, Topic: &topic},
		},
		Control: &ps_pb.TraceEvent_ControlMeta{
			Ihave: []*ps_pb.TraceEvent_ControlIHaveMeta{{Topic: &topic, MessageIDs: [][]byte{msgID1}}},
			Iwant: []*ps_pb.TraceEvent_ControlIWantMeta{{MessageIDs: [][]byte{msgID2}}},
		},
		Subscription: []*ps_pb.TraceEvent_SubMeta{{Topic: &topic}},
	}

	trace := func(evt *ps_pb.TraceEvent) map[string]any {
		core, logs := observer.New(zapcore.DebugLevel)
		newTracer(zap.New(core)).Trace(evt)
		entries := logs.All()
		require.Len(t, entries, 1, "expected exactly one trace log line")
		return entries[0].ContextMap()
	}

	t.Run("SEND_RPC logs payload msg ids, control ids and subs", func(t *testing.T) {
		fields := trace(&ps_pb.TraceEvent{
			Type:    ps_pb.TraceEvent_SEND_RPC.Enum(),
			SendRPC: &ps_pb.TraceEvent_SendRPC{SendTo: []byte(pid), Meta: fullMeta},
		})
		require.Equal(t, pid.String(), fields["targetPeer"])
		require.Equal(t, int64(2), fields["msgsCount"])
		require.Equal(t, []any{hex.EncodeToString(msgID1), hex.EncodeToString(msgID2)}, fields["msgIDs"])
		require.Equal(t, []any{hex.EncodeToString(msgID1)}, fields["IHAVEmsgIDs"])
		require.Equal(t, []any{hex.EncodeToString(msgID2)}, fields["IWANTmsgIDs"])
		require.Equal(t, int64(1), fields["subsCount"])
		require.Equal(t, []any{topic}, fields["subs"])
	})

	t.Run("RECV_RPC logs payload msg ids", func(t *testing.T) {
		fields := trace(&ps_pb.TraceEvent{
			Type: ps_pb.TraceEvent_RECV_RPC.Enum(),
			RecvRPC: &ps_pb.TraceEvent_RecvRPC{
				ReceivedFrom: []byte(pid),
				Meta: &ps_pb.TraceEvent_RPCMeta{
					Messages: []*ps_pb.TraceEvent_MessageMeta{{MessageID: msgID1, Topic: &topic}},
				},
			},
		})
		require.Equal(t, pid.String(), fields["receivedFrom"])
		require.Equal(t, int64(1), fields["msgsCount"])
		require.Equal(t, []any{hex.EncodeToString(msgID1)}, fields["msgIDs"])
		require.NotContains(t, fields, "IHAVEmsgIDs")
	})

	t.Run("DROP_RPC tolerates nil meta", func(t *testing.T) {
		fields := trace(&ps_pb.TraceEvent{
			Type:    ps_pb.TraceEvent_DROP_RPC.Enum(),
			DropRPC: &ps_pb.TraceEvent_DropRPC{SendTo: []byte(pid)},
		})
		require.Equal(t, pid.String(), fields["targetPeer"])
		require.NotContains(t, fields, "msgsCount")
		require.NotContains(t, fields, "subsCount")
	})

	t.Run("empty meta logs no msg ids but keeps subs fields", func(t *testing.T) {
		fields := trace(&ps_pb.TraceEvent{
			Type:    ps_pb.TraceEvent_SEND_RPC.Enum(),
			SendRPC: &ps_pb.TraceEvent_SendRPC{SendTo: []byte(pid), Meta: &ps_pb.TraceEvent_RPCMeta{}},
		})
		require.NotContains(t, fields, "msgsCount")
		require.Equal(t, int64(0), fields["subsCount"])
	})
}
