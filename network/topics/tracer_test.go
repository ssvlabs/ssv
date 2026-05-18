package topics

import (
	"testing"

	"github.com/gogo/protobuf/proto"
	ps_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	zapobserver "go.uber.org/zap/zaptest/observer"
)

func TestPsTracerLogRecvRPCMetadata(t *testing.T) {
	core, logs := zapobserver.New(zap.DebugLevel)
	tracer := &psTracer{
		logger:   zap.New(core),
		traceLog: true,
	}

	pid, err := peer.Decode("12D3KooWGRZpEouTWybB5jDKsVLqYXn3hXyzuTNxti4ghui6u5HE")
	require.NoError(t, err)
	tracer.Trace(&ps_pb.TraceEvent{
		Type: protoTraceEventType(ps_pb.TraceEvent_RECV_RPC),
		RecvRPC: &ps_pb.TraceEvent_RecvRPC{
			ReceivedFrom: []byte(pid),
			Meta: &ps_pb.TraceEvent_RPCMeta{
				Messages: []*ps_pb.TraceEvent_MessageMeta{
					{
						MessageID: []byte{0x01, 0x02},
						Topic:     proto.String("topic-a"),
					},
				},
				Subscription: []*ps_pb.TraceEvent_SubMeta{
					{Topic: proto.String("topic-b")},
				},
				Control: &ps_pb.TraceEvent_ControlMeta{
					Ihave: []*ps_pb.TraceEvent_ControlIHaveMeta{
						{
							Topic:      proto.String("topic-a"),
							MessageIDs: [][]byte{{0x03}},
						},
					},
					Iwant: []*ps_pb.TraceEvent_ControlIWantMeta{
						{MessageIDs: [][]byte{{0x04}}},
					},
					Graft: []*ps_pb.TraceEvent_ControlGraftMeta{
						{Topic: proto.String("topic-c")},
					},
					Prune: []*ps_pb.TraceEvent_ControlPruneMeta{
						{
							Topic: proto.String("topic-d"),
							Peers: [][]byte{[]byte(peer.ID("peer-b")), []byte(peer.ID("peer-c"))},
						},
					},
				},
			},
		},
	})

	require.Len(t, logs.All(), 1)
	fields := logs.All()[0].ContextMap()
	require.Equal(t, ps_pb.TraceEvent_RECV_RPC.String(), fields["type"])
	require.Equal(t, pid.String(), fields["receivedFrom"])
	require.Equal(t, int64(1), fields["messageCount"])
	require.Equal(t, []interface{}{"topic-a"}, fields["messageTopics"])
	require.Equal(t, []interface{}{"0102"}, fields["messageIDs"])
	require.Equal(t, int64(1), fields["subsCount"])
	require.Equal(t, []interface{}{"topic-b"}, fields["subs"])
	require.Equal(t, int64(1), fields["ihaveCount"])
	require.Equal(t, []interface{}{"03"}, fields["IHAVEmsgIDs"])
	require.Equal(t, int64(1), fields["iwantCount"])
	require.Equal(t, []interface{}{"04"}, fields["IWANTmsgIDs"])
	require.Equal(t, int64(1), fields["graftCount"])
	require.Equal(t, []interface{}{"topic-c"}, fields["graftTopics"])
	require.Equal(t, int64(1), fields["pruneCount"])
	require.Equal(t, []interface{}{"topic-d"}, fields["pruneTopics"])
	require.Equal(t, int64(2), fields["prunePeerCount"])
}

func TestAppendTraceMetadataSkipsEmptyInputs(t *testing.T) {
	require.Empty(t, appendMessages(nil, nil))
	require.Empty(t, appendIHave(nil, nil))
	require.Empty(t, appendIWant(nil, nil))
	require.Empty(t, appendGraft(nil, nil))
	require.Empty(t, appendPrune(nil, nil))
}

func protoTraceEventType(eventType ps_pb.TraceEvent_Type) *ps_pb.TraceEvent_Type {
	return &eventType
}
