package v0

import "github.com/libp2p/go-libp2p-core/protocol"

const (
	legacyMsgStream = "/sync/0.0.1"

	peersForSync = 4
)

func (v0 *ForkV0) LastDecidedProtocol() (protocol.ID, int) {
	return legacyMsgStream, peersForSync
}

func (v0 *ForkV0) LastChangeRoundProtocol() (protocol.ID, int) {
	return legacyMsgStream, peersForSync
}

func (v0 *ForkV0) DecidedHistoryProtocol() (protocol.ID, int) {
	return legacyMsgStream, peersForSync
}
