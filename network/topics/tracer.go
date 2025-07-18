package topics

import (
	"encoding/hex"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	ps_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
)

// psTracer helps to trace pubsub events
// it can run with logging in addition to reporting (on by default)
type psTracer struct {
	logger *zap.Logger // struct logger to implement pubsub.EventTracer
}

// newTracer creates an instance of psTracer
func newTracer(logger *zap.Logger) pubsub.EventTracer {
	return &psTracer{logger: logger.Named(logging.NamePubsubTrace)}
}

// Trace handles events, implementation of pubsub.EventTracer
func (pst *psTracer) Trace(evt *ps_pb.TraceEvent) {
	pst.log(pst.logger, evt)
}

// log prints event to log
func (pst *psTracer) log(logger *zap.Logger, evt *ps_pb.TraceEvent) {
	if evt == nil {
		return
	}
	fields := []zap.Field{
		zap.String("type", evt.GetType().String()),
	}
	switch evt.GetType() {
	case ps_pb.TraceEvent_PUBLISH_MESSAGE:
		msg := evt.GetPublishMessage()
		fields = append(fields, zap.String("msgID", hex.EncodeToString(msg.GetMessageID())))
		fields = append(fields, zap.String("topic", msg.GetTopic()))
	case ps_pb.TraceEvent_REJECT_MESSAGE:
		msg := evt.GetRejectMessage()
		pid, err := peer.IDFromBytes(msg.GetReceivedFrom())
		if err == nil {
			fields = append(fields, zap.String("receivedFrom", pid.String()))
		}
		fields = append(fields, zap.String("msgID", hex.EncodeToString(msg.GetMessageID())))
		fields = append(fields, zap.String("topic", msg.GetTopic()))
		fields = append(fields, zap.String("reason", msg.GetReason()))
	case ps_pb.TraceEvent_DUPLICATE_MESSAGE:
		msg := evt.GetDuplicateMessage()
		pid, err := peer.IDFromBytes(msg.GetReceivedFrom())
		if err == nil {
			fields = append(fields, zap.String("receivedFrom", pid.String()))
		}
		fields = append(fields, zap.String("msgID", hex.EncodeToString(msg.GetMessageID())))
		fields = append(fields, zap.String("topic", msg.GetTopic()))
	case ps_pb.TraceEvent_DELIVER_MESSAGE:
		msg := evt.GetDeliverMessage()
		pid, err := peer.IDFromBytes(msg.GetReceivedFrom())
		if err == nil {
			fields = append(fields, zap.String("receivedFrom", pid.String()))
		}
		fields = append(fields, zap.String("msgID", hex.EncodeToString(msg.GetMessageID())))
		fields = append(fields, zap.String("topic", msg.GetTopic()))
	case ps_pb.TraceEvent_ADD_PEER:
		pid, err := peer.IDFromBytes(evt.GetAddPeer().GetPeerID())
		if err == nil {
			fields = append(fields, zap.String("targetPeer", pid.String()))
		}
	case ps_pb.TraceEvent_REMOVE_PEER:
		pid, err := peer.IDFromBytes(evt.GetRemovePeer().GetPeerID())
		if err == nil {
			fields = append(fields, zap.String("targetPeer", pid.String()))
		}
	case ps_pb.TraceEvent_JOIN:
		fields = append(fields, zap.String("topic", evt.GetJoin().GetTopic()))
	case ps_pb.TraceEvent_LEAVE:
		fields = append(fields, zap.String("topic", evt.GetLeave().GetTopic()))
	case ps_pb.TraceEvent_GRAFT:
		msg := evt.GetGraft()
		pid, err := peer.IDFromBytes(msg.GetPeerID())
		if err == nil {
			fields = append(fields, zap.String("graftPeer", pid.String()))
		}
		fields = append(fields, zap.String("topic", msg.GetTopic()))
	case ps_pb.TraceEvent_PRUNE:
		msg := evt.GetPrune()
		pid, err := peer.IDFromBytes(msg.GetPeerID())
		if err == nil {
			fields = append(fields, zap.String("prunePeer", pid.String()))
		}
		fields = append(fields, zap.String("topic", msg.GetTopic()))
	case ps_pb.TraceEvent_SEND_RPC:
		msg := evt.GetSendRPC()
		pid, err := peer.IDFromBytes(msg.GetSendTo())
		if err == nil {
			fields = append(fields, zap.String("targetPeer", pid.String()))
		}
		if meta := msg.GetMeta(); meta != nil {
			if ctrl := meta.Control; ctrl != nil {
				fields = appendIHave(fields, ctrl.GetIhave())
				fields = appendIWant(fields, ctrl.GetIwant())
			}
			var subs []string
			for _, sub := range meta.Subscription {
				subs = append(subs, sub.GetTopic())
			}
			fields = append(fields, zap.Int("subsCount", len(subs)))
			fields = append(fields, zap.Strings("subs", subs))
		}
	case ps_pb.TraceEvent_DROP_RPC:
		msg := evt.GetDropRPC()
		pid, err := peer.IDFromBytes(msg.GetSendTo())
		if err == nil {
			fields = append(fields, zap.String("targetPeer", pid.String()))
		}
		if meta := msg.GetMeta(); meta != nil {
			if ctrl := meta.Control; ctrl != nil {
				fields = appendIHave(fields, ctrl.GetIhave())
				fields = appendIWant(fields, ctrl.GetIwant())
			}
			var subs []string
			for _, sub := range meta.Subscription {
				subs = append(subs, sub.GetTopic())
			}
			fields = append(fields, zap.Int("subsCount", len(subs)))
			fields = append(fields, zap.Strings("subs", subs))
		}
	case ps_pb.TraceEvent_RECV_RPC:
		msg := evt.GetRecvRPC()
		pid, err := peer.IDFromBytes(msg.GetReceivedFrom())
		if err == nil {
			fields = append(fields, zap.String("receivedFrom", pid.String()))
		}
		if meta := msg.GetMeta(); meta != nil {
			if ctrl := meta.Control; ctrl != nil {
				fields = appendIHave(fields, ctrl.GetIhave())
				fields = appendIWant(fields, ctrl.GetIwant())
			}
			var subs []string
			for _, sub := range meta.Subscription {
				subs = append(subs, sub.GetTopic())
			}
			fields = append(fields, zap.Int("subsCount", len(subs)))
			fields = append(fields, zap.Strings("subs", subs))
		}
	default:
		return
	}
	logger.Debug("pubsub event", fields...)
}

func appendIHave(fields []zap.Field, ihave []*ps_pb.TraceEvent_ControlIHaveMeta) []zap.Field {
	if len(ihave) > 0 {
		fields = append(fields, zap.Int("ihaveCount", len(ihave)))
		for _, im := range ihave {
			var mids []string
			msgids := im.GetMessageIDs()
			for _, mid := range msgids {
				mids = append(mids, hex.EncodeToString(mid))
			}
			fields = append(fields, zap.Strings("IHAVEmsgIDs", mids))
		}
	}
	return fields
}

func appendIWant(fields []zap.Field, iwant []*ps_pb.TraceEvent_ControlIWantMeta) []zap.Field {
	if len(iwant) > 0 {
		fields = append(fields, zap.Int("iwantCount", len(iwant)))
		for _, im := range iwant {
			var mids []string
			msgids := im.GetMessageIDs()
			for _, mid := range msgids {
				mids = append(mids, hex.EncodeToString(mid))
			}
			fields = append(fields, zap.Strings("IWANTmsgIDs", mids))
		}
	}
	return fields
}
