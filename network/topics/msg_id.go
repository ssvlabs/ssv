package topics

import (
	"encoding/binary"

	"github.com/cespare/xxhash/v2"
	ps_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"
)

const (
	// MsgIDEmptyMessage is the msg_id for empty messages
	MsgIDEmptyMessage = "invalid:empty"
	// MsgIDBadPeerID is the msg_id for messages without a parseable From
	MsgIDBadPeerID = "invalid:peer_id_error"
)

// PubsubMsgID is gossipsub's message-id function: the msg_id of a pubsub message
// from its content. It keeps no state: From is not authenticated (StrictNoSign), so
// the id depends on the content only.
//
// From is only checked for being a parseable peer ID. A message without one gets the
// shared id MsgIDBadPeerID. Nodes must agree on every msg_id, so changing or dropping
// the shared id has to happen on all nodes at once, at a fork.
func PubsubMsgID(pMsg *ps_pb.Message) string {
	data := pMsg.GetData()
	if len(data) == 0 {
		return MsgIDEmptyMessage
	}
	if _, err := peer.IDFromBytes(pMsg.GetFrom()); err != nil {
		return MsgIDBadPeerID
	}
	return MsgID(data)
}

// MsgID computes the gossipsub message-id for a raw pubsub payload, identical to the id gossipsub
// itself assigns to a non-empty message with a valid From. Exported so the broadcast path can log the same id a receiver (and the pubsub
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
