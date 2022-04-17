package v0

import (
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/libp2p/go-libp2p-core/protocol"
)

const (
	legacyMsgStream = "/sync/0.0.1"

	peersForSync    = 4
	peersForHistory = 1
)

// ProtocolID returns the protocol id of the given protocol,
// and the amount of peers for distribution
func (v0 *ForkV0) ProtocolID(prot p2pprotocol.SyncProtocol) (protocol.ID, int) {
	switch prot {
	case p2pprotocol.LastDecidedProtocol:
		return legacyMsgStream, peersForSync
	case p2pprotocol.LastChangeRoundProtocol:
		return legacyMsgStream, peersForSync
	case p2pprotocol.DecidedHistoryProtocol:
		return legacyMsgStream, peersForHistory
	}
	return "", 0
}
