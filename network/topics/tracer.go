package topics

import (
	"context"
	"encoding/hex"
	"strings"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	ps_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/peers/peertrace"
	"github.com/ssvlabs/ssv/observability/log"
)

// psTracer helps to trace pubsub events
// it can run with logging in addition to reporting (on by default)
type psTracer struct {
	logger       *zap.Logger // struct logger to implement pubsub.EventTracer
	ctx          context.Context
	traceLog     bool
	peerObserver *peertrace.Observer
}

// newTracer creates an instance of psTracer
func newTracer(ctx context.Context, logger *zap.Logger, traceLog bool, peerObserver *peertrace.Observer) pubsub.EventTracer {
	return &psTracer{
		logger:       logger.Named(log.NamePubsubTrace),
		ctx:          ctx,
		traceLog:     traceLog,
		peerObserver: peerObserver,
	}
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
	eventPeer := tracedEventPeer(evt)
	_, highlighted := pst.peerObserver.Match(eventPeer)
	// Building the fields below is not free, so skip it entirely when nothing
	// would consume them (highlighted-peer-only mode, event from a regular peer).
	if !pst.traceLog && !highlighted {
		return
	}
	fields := []zap.Field{
		zap.String("type", evt.GetType().String()),
	}
	// appendRPCMeta keeps the long-standing trace-log schema (IHAVE/IWANT and
	// subscriptions) for every peer and adds the more verbose message/GRAFT/PRUNE
	// metadata only for highlighted peers.
	appendRPCMeta := func(meta *ps_pb.TraceEvent_RPCMeta) {
		if meta == nil {
			return
		}
		if highlighted {
			fields = appendMessages(fields, meta.GetMessages())
		}
		if ctrl := meta.Control; ctrl != nil {
			fields = appendIHave(fields, ctrl.GetIhave())
			fields = appendIWant(fields, ctrl.GetIwant())
			if highlighted {
				fields = appendGraft(fields, ctrl.GetGraft())
				fields = appendPrune(fields, ctrl.GetPrune())
			}
		}
		var subs []string
		for _, sub := range meta.Subscription {
			subs = append(subs, sub.GetTopic())
		}
		fields = append(fields, zap.Int("subsCount", len(subs)))
		fields = append(fields, zap.Strings("subs", subs))
	}
	switch evt.GetType() {
	case ps_pb.TraceEvent_PUBLISH_MESSAGE:
		msg := evt.GetPublishMessage()
		fields = append(fields, zap.String("msgID", hex.EncodeToString(msg.GetMessageID())))
		fields = append(fields, zap.String("topic", msg.GetTopic()))
	case ps_pb.TraceEvent_REJECT_MESSAGE:
		msg := evt.GetRejectMessage()
		if eventPeer != "" {
			fields = append(fields, zap.String("receivedFrom", eventPeer.String()))
		}
		fields = append(fields, zap.String("msgID", hex.EncodeToString(msg.GetMessageID())))
		fields = append(fields, zap.String("topic", msg.GetTopic()))
		fields = append(fields, zap.String("reason", msg.GetReason()))
	case ps_pb.TraceEvent_DUPLICATE_MESSAGE:
		msg := evt.GetDuplicateMessage()
		if eventPeer != "" {
			fields = append(fields, zap.String("receivedFrom", eventPeer.String()))
		}
		fields = append(fields, zap.String("msgID", hex.EncodeToString(msg.GetMessageID())))
		fields = append(fields, zap.String("topic", msg.GetTopic()))
	case ps_pb.TraceEvent_DELIVER_MESSAGE:
		msg := evt.GetDeliverMessage()
		if eventPeer != "" {
			fields = append(fields, zap.String("receivedFrom", eventPeer.String()))
		}
		fields = append(fields, zap.String("msgID", hex.EncodeToString(msg.GetMessageID())))
		fields = append(fields, zap.String("topic", msg.GetTopic()))
	case ps_pb.TraceEvent_ADD_PEER:
		if eventPeer != "" {
			fields = append(fields, zap.String("targetPeer", eventPeer.String()))
		}
	case ps_pb.TraceEvent_REMOVE_PEER:
		if eventPeer != "" {
			fields = append(fields, zap.String("targetPeer", eventPeer.String()))
		}
	case ps_pb.TraceEvent_JOIN:
		fields = append(fields, zap.String("topic", evt.GetJoin().GetTopic()))
	case ps_pb.TraceEvent_LEAVE:
		fields = append(fields, zap.String("topic", evt.GetLeave().GetTopic()))
	case ps_pb.TraceEvent_GRAFT:
		if eventPeer != "" {
			fields = append(fields, zap.String("graftPeer", eventPeer.String()))
		}
		fields = append(fields, zap.String("topic", evt.GetGraft().GetTopic()))
	case ps_pb.TraceEvent_PRUNE:
		if eventPeer != "" {
			fields = append(fields, zap.String("prunePeer", eventPeer.String()))
		}
		fields = append(fields, zap.String("topic", evt.GetPrune().GetTopic()))
	case ps_pb.TraceEvent_SEND_RPC:
		if eventPeer != "" {
			fields = append(fields, zap.String("targetPeer", eventPeer.String()))
		}
		appendRPCMeta(evt.GetSendRPC().GetMeta())
	case ps_pb.TraceEvent_DROP_RPC:
		if eventPeer != "" {
			fields = append(fields, zap.String("targetPeer", eventPeer.String()))
		}
		appendRPCMeta(evt.GetDropRPC().GetMeta())
	case ps_pb.TraceEvent_RECV_RPC:
		if eventPeer != "" {
			fields = append(fields, zap.String("receivedFrom", eventPeer.String()))
		}
		appendRPCMeta(evt.GetRecvRPC().GetMeta())
	default:
		return
	}
	if highlighted {
		pst.peerObserver.Observe(pst.context(), logger, "pubsub_trace_"+strings.ToLower(evt.GetType().String()), eventPeer, fields...)
	}
	if pst.traceLog {
		logger.Debug("pubsub event", fields...)
	}
}

// tracedEventPeer extracts the remote peer a trace event relates to, if any.
func tracedEventPeer(evt *ps_pb.TraceEvent) peer.ID {
	var raw []byte
	switch evt.GetType() {
	case ps_pb.TraceEvent_REJECT_MESSAGE:
		raw = evt.GetRejectMessage().GetReceivedFrom()
	case ps_pb.TraceEvent_DUPLICATE_MESSAGE:
		raw = evt.GetDuplicateMessage().GetReceivedFrom()
	case ps_pb.TraceEvent_DELIVER_MESSAGE:
		raw = evt.GetDeliverMessage().GetReceivedFrom()
	case ps_pb.TraceEvent_ADD_PEER:
		raw = evt.GetAddPeer().GetPeerID()
	case ps_pb.TraceEvent_REMOVE_PEER:
		raw = evt.GetRemovePeer().GetPeerID()
	case ps_pb.TraceEvent_GRAFT:
		raw = evt.GetGraft().GetPeerID()
	case ps_pb.TraceEvent_PRUNE:
		raw = evt.GetPrune().GetPeerID()
	case ps_pb.TraceEvent_SEND_RPC:
		raw = evt.GetSendRPC().GetSendTo()
	case ps_pb.TraceEvent_DROP_RPC:
		raw = evt.GetDropRPC().GetSendTo()
	case ps_pb.TraceEvent_RECV_RPC:
		raw = evt.GetRecvRPC().GetReceivedFrom()
	}
	if len(raw) == 0 {
		return ""
	}
	pid, err := peer.IDFromBytes(raw)
	if err != nil {
		return ""
	}
	return pid
}

func (pst *psTracer) context() context.Context {
	if pst.ctx == nil {
		return context.Background()
	}
	return pst.ctx
}

func appendMessages(fields []zap.Field, messages []*ps_pb.TraceEvent_MessageMeta) []zap.Field {
	if len(messages) == 0 {
		return fields
	}

	topics := make([]string, 0, len(messages))
	msgIDs := make([]string, 0, len(messages))
	for _, msg := range messages {
		topics = append(topics, msg.GetTopic())
		msgIDs = append(msgIDs, hex.EncodeToString(msg.GetMessageID()))
	}
	return append(fields,
		zap.Int("messageCount", len(messages)),
		zap.Strings("messageTopics", topics),
		zap.Strings("messageIDs", msgIDs),
	)
}

func appendIHave(fields []zap.Field, ihave []*ps_pb.TraceEvent_ControlIHaveMeta) []zap.Field {
	if len(ihave) > 0 {
		fields = append(fields, zap.Int("ihaveCount", len(ihave)))
		for _, im := range ihave {
			msgids := im.GetMessageIDs()
			mids := make([]string, 0, len(msgids))
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
			msgids := im.GetMessageIDs()
			mids := make([]string, 0, len(msgids))
			for _, mid := range msgids {
				mids = append(mids, hex.EncodeToString(mid))
			}
			fields = append(fields, zap.Strings("IWANTmsgIDs", mids))
		}
	}
	return fields
}

func appendGraft(fields []zap.Field, graft []*ps_pb.TraceEvent_ControlGraftMeta) []zap.Field {
	if len(graft) == 0 {
		return fields
	}

	topics := make([]string, 0, len(graft))
	for _, gm := range graft {
		topics = append(topics, gm.GetTopic())
	}
	return append(fields,
		zap.Int("graftCount", len(graft)),
		zap.Strings("graftTopics", topics),
	)
}

func appendPrune(fields []zap.Field, prune []*ps_pb.TraceEvent_ControlPruneMeta) []zap.Field {
	if len(prune) == 0 {
		return fields
	}

	topics := make([]string, 0, len(prune))
	peerCount := 0
	for _, pm := range prune {
		topics = append(topics, pm.GetTopic())
		peerCount += len(pm.GetPeers())
	}
	return append(fields,
		zap.Int("pruneCount", len(prune)),
		zap.Strings("pruneTopics", topics),
		zap.Int("prunePeerCount", peerCount),
	)
}
