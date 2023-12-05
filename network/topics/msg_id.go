package topics

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/networkconfig"

	ps_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"
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

const (
	msgIDHandlerBufferSize = 32
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
	ctx           context.Context
	added         chan addedEvent
	ids           map[string]*msgIDEntry
	locker        sync.Locker
	ttl           time.Duration
	networkConfig networkconfig.NetworkConfig
}

// NewMsgIDHandler creates a new MsgIDHandler
func NewMsgIDHandler(ctx context.Context, ttl time.Duration, networkConfig networkconfig.NetworkConfig) MsgIDHandler {
	handler := &msgIDHandler{
		ctx:           ctx,
		added:         make(chan addedEvent, msgIDHandlerBufferSize),
		ids:           make(map[string]*msgIDEntry),
		locker:        &sync.Mutex{},
		ttl:           ttl,
		networkConfig: networkConfig,
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
	return func(pmsg *ps_pb.Message) string {
		if pmsg == nil {
			return MsgIDEmptyMessage
		}

		messageData := pmsg.GetData()
		if len(messageData) == 0 {
			return MsgIDEmptyMessage
		}

		pid, err := peer.IDFromBytes(pmsg.GetFrom())
		if err != nil {
			return MsgIDBadPeerID
		}

		mid := handler.pubsubMsgToMsgID(messageData)

		if len(mid) == 0 {
			logger.Debug("could not create msg_id",
				zap.ByteString("seq_no", pmsg.GetSeqno()),
				fields.PeerID(pid))
			return MsgIDError
		}

		handler.Add(mid, pid)
		return mid
	}
}

func (handler *msgIDHandler) pubsubMsgToMsgID(msg []byte) string {
	currentEpoch := handler.networkConfig.Beacon.EstimatedCurrentEpoch()
	if currentEpoch > handler.networkConfig.PermissionlessActivationEpoch {
		decodedMsg, _, _, err := commons.DecodeSignedSSVMessage(msg)
		if err != nil {
			// todo: should err here or just log and let the decode function err?
		} else {
			return commons.MsgID()(decodedMsg)
		}
	}
	return commons.MsgID()(msg)
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
// it uses channel to avoid blocking
func (handler *msgIDHandler) Add(msgID string, pi peer.ID) {
	select {
	case handler.added <- addedEvent{
		mid: msgID,
		pid: pi,
	}:
	default:
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
