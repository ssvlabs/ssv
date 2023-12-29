package topics

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
	pubsub_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"
)

type Metrics interface {
	PeerScore(peer.ID, float64)
	PeerP4Score(peer.ID, float64)
	ResetPeerScores()
	PubsubTrace(eventType pubsub_pb.TraceEvent_Type)
	PubsubOutbound(topicName string)
	PubsubInbound(topicName string, msgType spectypes.MsgType)
}
