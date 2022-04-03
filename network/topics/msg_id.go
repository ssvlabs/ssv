package topics

import (
	"bytes"
	"github.com/bloxapp/ssv/network/forks"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/libp2p/go-libp2p-core/peer"
	ps_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"go.uber.org/zap"
	"sync"
	"time"
)

const (
	// MsgIDEmptyMessage is the msg_id for empty messages
	MsgIDEmptyMessage = "invalid:empty"
	// MsgIDBadEncodedMessage is the msg_id for messages with invalid encoding
	MsgIDBadEncodedMessage = "invalid:encoding"
	// MsgIDError is the msg_id for messages that we can't create their msg_id
	MsgIDError = "invalid:msg_id_error"
	// MsgIDBadPeerID is the msg_id for messages w/o a valid sender
	MsgIDBadPeerID = "invalid:peer_id_error"
)

// MsgPeersResolver will resolve the sending peers of the given message
type MsgPeersResolver interface {
	GetPeers(msg []byte) []peer.ID
}

// MsgIDHandler stores msgIDs and the corresponding sender peer.ID
// it works in memory as this store is expected to be invoked a lot, adding msgID and peerID pairs for every message
// this uses to identify msg senders after validation
type MsgIDHandler interface {
	MsgPeersResolver

	MsgID() func(pmsg *ps_pb.Message) string
	GC()
}

// msgIDEntry is a wrapper object that includes the sending peers and timing for expiration
type msgIDEntry struct {
	peers []peer.ID
	t     time.Time
}

// msgIDHandler implements MsgIDHandler
type msgIDHandler struct {
	logger *zap.Logger
	fork   forks.Fork
	ids    map[string]*msgIDEntry
	locker sync.Locker
	ttl    time.Duration
}

// NewMsgIDHandler creates a new MsgIDHandler
func NewMsgIDHandler(logger *zap.Logger, fork forks.Fork, ttl time.Duration) MsgIDHandler {
	return &msgIDHandler{
		logger: logger,
		fork:   fork,
		ids:    make(map[string]*msgIDEntry),
		locker: &sync.Mutex{},
		ttl:    ttl,
	}
}

// MsgID returns the msg_id function that calculates msg_id based on it's content
func (handler *msgIDHandler) MsgID() func(pmsg *ps_pb.Message) string {
	return func(pmsg *ps_pb.Message) string {
		if pmsg == nil {
			return MsgIDEmptyMessage
		}
		logger := handler.logger.With(zap.ByteString("seq_no", pmsg.GetSeqno()))
		if len(pmsg.GetData()) == 0 {
			logger.Warn("empty message", zap.ByteString("pmsg.From", pmsg.GetFrom()))
			//return fmt.Sprintf("%s/%s", MsgIDEmptyMessage, pubsub.DefaultMsgIdFn(pmsg))
			return MsgIDEmptyMessage
		}
		pid, err := peer.IDFromBytes(pmsg.GetFrom())
		if err != nil {
			logger.Warn("could not convert sender to peer id",
				zap.ByteString("pmsg.From", pmsg.GetFrom()), zap.Error(err))
			return MsgIDBadPeerID
		}
		logger = logger.With(zap.String("from", pid.String()))
		msg, err := handler.fork.DecodeNetworkMsg(pmsg.GetData())
		if err != nil {
			logger.Warn("invalid encoding", zap.Error(err))
			return MsgIDBadEncodedMessage
		}
		ssvMsg, ok := msg.(*message.SSVMessage)
		if !ok {
			logger.Warn("invalid encoding: could not cast to ssv message", zap.Error(err))
			return MsgIDBadEncodedMessage
		}
		mid := handler.fork.MsgID()(ssvMsg.Data)
		if len(mid) == 0 {
			logger.Warn("could not create msg_id")
			return MsgIDError
		}
		handler.add(mid, pid)
		//logger.Debug("msg_id created", zap.String("value", mid))
		return mid
	}
}

// GetPeers returns the peers that are related to the given msg
func (handler *msgIDHandler) GetPeers(msg []byte) []peer.ID {
	msgID := handler.fork.MsgID()(msg)
	handler.locker.Lock()
	defer handler.locker.Unlock()
	entry, ok := handler.ids[msgID]
	if ok {
		if !entry.t.Add(handler.ttl).After(time.Now()) {
			return entry.peers
		}
		// otherwise -> expired
		delete(handler.ids, msgID)
	}
	return []peer.ID{}
}

// add the pair of msg id and peer id
func (handler *msgIDHandler) add(msgID string, pi peer.ID) {
	handler.locker.Lock()
	defer handler.locker.Unlock()

	entry, ok := handler.ids[msgID]
	if !ok {
		entry = &msgIDEntry{
			peers: []peer.ID{},
		}
	}
	// extend expiration
	entry.t = time.Now()
	// add the peer
	b := []byte(pi)
	for _, p := range entry.peers {
		if bytes.Equal([]byte(p), b) {
			return
		}
	}
	entry.peers = append(entry.peers, pi)
}

// GC performs garbage collection on the given map
func (handler *msgIDHandler) GC() {
	handler.locker.Lock()
	defer handler.locker.Unlock()

	ids := make(map[string]*msgIDEntry)
	for m, entry := range handler.ids {
		if entry.t.Add(handler.ttl).After(time.Now()) {
			ids[m] = entry
		}
	}
	handler.ids = ids
}
