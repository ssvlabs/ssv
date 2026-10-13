package topics

import (
	"bytes"
	"context"
	"encoding/binary"
	"sync"
	"time"

	"github.com/cespare/xxhash/v2"
	ps_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log/fields"
)

const (
	// MsgIDEmptyMessage is the msg_id for empty messages
	MsgIDEmptyMessage = "invalid:empty"
	// MsgIDError is the msg_id for messages that we can't create their msg_id
	MsgIDError = "invalid:msg_id_error"
	// MsgIDBadPeerID is the msg_id for messages w/o a valid sender
	MsgIDBadPeerID = "invalid:peer_id_error"
)

const (
	msgIDHandlerBufferSize = 1024
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
	MsgID(logger *zap.Logger) func(pmsg *ps_pb.Message) string

	Start()
	GC()
}

// msgIDEntry is a wrapper object that includes the sending peers and timing for expiration
type msgIDEntry struct {
	peers []peer.ID
	t     time.Time
}

// msgIDHandler implements MsgIDHandler
type msgIDHandler struct {
	ctx    context.Context
	added  chan addedEvent
	ids    map[string]*msgIDEntry
	locker sync.Locker
	ttl    time.Duration
}

// NewMsgIDHandler creates a new MsgIDHandler
func NewMsgIDHandler(ctx context.Context, ttl time.Duration) MsgIDHandler {
	handler := &msgIDHandler{
		ctx:    ctx,
		added:  make(chan addedEvent, msgIDHandlerBufferSize),
		ids:    make(map[string]*msgIDEntry),
		locker: &sync.Mutex{},
		ttl:    ttl,
	}
	return handler
}

type addedEvent struct {
	mid string
	pid peer.ID
}

func (handler *msgIDHandler) Start() {
	lctx, cancel := context.WithCancel(handler.ctx)
	defer cancel()
	for {
		select {
		case e := <-handler.added:
			handler.add(e.mid, e.pid)
		case <-lctx.Done():
			return
		}
	}
}

// MsgID returns the msg_id function that calculates msg_id based on it's content.
func (handler *msgIDHandler) MsgID(logger *zap.Logger) func(pmsg *ps_pb.Message) string {
	return func(pMsg *ps_pb.Message) string {
		if pMsg == nil {
			return MsgIDEmptyMessage
		}

		messageData := pMsg.GetData()
		if len(messageData) == 0 {
			return MsgIDEmptyMessage
		}

		peerID, err := peer.IDFromBytes(pMsg.GetFrom())
		if err != nil {
			return MsgIDBadPeerID
		}

		msgID := handler.pubsubMsgToMsgID(messageData)

		if len(msgID) == 0 {
			logger.Debug("could not create msg_id",
				zap.ByteString("seq_no", pMsg.GetSeqno()),
				fields.PeerID(peerID),
			)
			return MsgIDError
		}

		handler.Add(msgID, peerID)
		return msgID
	}
}

func (handler *msgIDHandler) pubsubMsgToMsgID(msg []byte) string {
	return MsgID(msg)
}

// MsgID computes the gossipsub message-id for a raw pubsub payload, identical to the id gossipsub
// itself assigns. Exported so the broadcast path can log the same id a receiver (and the pubsub
// tracer) sees, for cross-node correlation.
//
// We hash the full SignedSSVMessage, not just its body. Under the Alan message structure the body
// can be byte-identical across a committee's operators — e.g. same-view prepare/commit messages, or
// a decided vs. a plain commit — so a body-only id would alias their distinct messages onto a single
// id. gossipsub marks an id "seen" before our validators run and silently drops later duplicates, so
// that aliasing would censor all but the first operator's message to arrive and deadlock consensus.
// Hashing the full message — which includes each operator's RSA signatures — keeps every message's id
// distinct; as a side benefit the ids also become unpredictable to an observer, avoiding seen-cache
// shadowing.
func MsgID(msg []byte) string {
	if len(msg) == 0 {
		return ""
	}
	b := make([]byte, 12)
	binary.LittleEndian.PutUint64(b, xxhash.Sum64(msg))
	return string(b)
}

// GetPeers returns the peers that are related to the given msg
func (handler *msgIDHandler) GetPeers(msg []byte) []peer.ID {
	msgID := handler.pubsubMsgToMsgID(msg)

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

// Add adds the given pair of msg id + peer id
// it uses a buffered channel to reduce lock contention and falls back to a
// synchronous insert when the buffer is full to avoid losing associations.
func (handler *msgIDHandler) Add(msgID string, pi peer.ID) {
	select {
	case handler.added <- addedEvent{
		mid: msgID,
		pid: pi,
	}:
	default:
		msgIDHandlerBufferFallbackCounter.Add(handler.ctx, 1)
		handler.add(msgID, pi)
	}
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
	// update entry
	entry.t = time.Now()
	b := []byte(pi)
	for _, p := range entry.peers {
		if bytes.Equal([]byte(p), b) {
			return
		}
	}
	entry.peers = append(entry.peers, pi)
	handler.ids[msgID] = entry
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
