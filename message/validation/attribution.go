package validation

import (
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
)

// observeAccepted reports an accepted message to the attribution observer when exactly one
// operator signed it. Gossipsub validates only the first copy of a message, so peerID is the
// first peer that delivered it. Multi-signer (decided) messages say nothing about a single
// operator and are skipped, as are this node's own messages.
func (mv *messageValidator) observeAccepted(peerID peer.ID, msg *queue.SSVMessage) {
	if mv.attribution == nil || peerID == "" || mv.isSelfPeer(peerID) {
		return
	}
	if msg == nil || msg.SignedSSVMessage == nil {
		return
	}
	signers := msg.SignedSSVMessage.OperatorIDs
	if len(signers) != 1 {
		return
	}
	mv.attribution.Observe(peerID, signers[0])
}
