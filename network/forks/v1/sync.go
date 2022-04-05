package v1

import "github.com/libp2p/go-libp2p-core/protocol"

const (
	lastDecidedProtocol = "/ssv/sync/decided/last/0.0.1"
	changeRoundProtocol = "/ssv/sync/round/0.0.1"
	historyProtocol     = "/ssv/sync/decided/history/0.0.1"

	peersForSync = 10
)

func (v1 *ForkV1) LastDecidedProtocol() (protocol.ID, int) {
	return lastDecidedProtocol, peersForSync
}

func (v1 *ForkV1) LastChangeRoundProtocol() (protocol.ID, int) {
	return changeRoundProtocol, peersForSync
}

func (v1 *ForkV1) DecidedHistoryProtocol() (protocol.ID, int) {
	return historyProtocol, peersForSync
}
