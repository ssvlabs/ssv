package validation

import (
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
)

// observeAccepted reports an accepted message to the attribution observer when exactly one
// operator signed it. The peer reported is the message's author: the From field the publishing
// node stamps on it, which every relay forwards unchanged. Unlike the peer that happened to
// deliver the message first, the author doesn't depend on mesh topology, so a well-connected
// relay isn't credited with other operators' messages. From isn't signed (pubsub runs with
// StrictNoSign), so a node could lie about it, but honest nodes don't. Multi-signer (decided)
// messages say nothing about a single operator and are skipped, as are this node's own messages
// and messages without a parseable author.
func (mv *messageValidator) observeAccepted(pmsg *pubsub.Message, msg *queue.SSVMessage) {
	if mv.attribution == nil || pmsg == nil || pmsg.Message == nil {
		return
	}
	if msg == nil || msg.SignedSSVMessage == nil {
		return
	}
	signers := msg.SignedSSVMessage.OperatorIDs
	if len(signers) != 1 {
		return
	}
	author, err := peer.IDFromBytes(pmsg.Message.GetFrom())
	if err != nil || author == "" || mv.isSelfPeer(author) {
		return
	}
	mv.attribution.Observe(author, signers[0])
}
