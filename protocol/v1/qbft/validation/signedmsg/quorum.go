package signedmsg

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
)

// HasQuorum checks if messages have a quorum.
func HasQuorum(share *beacon.Share, msgs []*specqbft.SignedMessage) (quorum bool, t int, n int) {
	uniqueSigners := make(map[spectypes.OperatorID]bool)
	for _, msg := range msgs {
		for _, signer := range msg.GetSigners() {
			uniqueSigners[signer] = true
		}
	}
	quorum = len(uniqueSigners)*3 >= share.CommitteeSize()*2
	return quorum, len(uniqueSigners), share.CommitteeSize()
}
