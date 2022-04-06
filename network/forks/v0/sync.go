package v0

import "github.com/libp2p/go-libp2p-core/protocol"

const (
	legacyMsgStream = "/sync/0.0.1"

	peersForSync = 4
	peersForHistory = 1
)

// LastDecidedProtocol returns the protocol id of last decided protocol,
// and the amount of peers for distribution
func (v0 *ForkV0) LastDecidedProtocol() (protocol.ID, int) {
	return legacyMsgStream, peersForSync
}

// LastChangeRoundProtocol returns the protocol id of last change round protocol,
// and the amount of peers for distribution
func (v0 *ForkV0) LastChangeRoundProtocol() (protocol.ID, int) {
	return legacyMsgStream, peersForSync
}

// DecidedHistoryProtocol returns the protocol id of decided history protocol,
// and the amount of peers for distribution
func (v0 *ForkV0) DecidedHistoryProtocol() (protocol.ID, int) {
	return legacyMsgStream, peersForHistory
}
