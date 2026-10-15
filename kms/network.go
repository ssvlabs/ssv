package kms

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	protocolp2p "github.com/ssvlabs/ssv/protocol/v2/p2p"
)

// committeeNetwork is the QBFT controller's view of the network for one key: everything goes to the
// key committee's topic. Slots don't apply to the dKMS.
type committeeNetwork struct {
	s         *Service
	committee []spectypes.OperatorID
}

var _ protocolp2p.Network = (*committeeNetwork)(nil)

func (n *committeeNetwork) Broadcast(_ spectypes.MessageID, msg *spectypes.SignedSSVMessage) error {
	return n.s.net.BroadcastToOperators(n.committee, msg)
}

func (n *committeeNetwork) BroadcastAtSlot(msg *spectypes.SignedSSVMessage, _ phase0.Slot) error {
	return n.s.net.BroadcastToOperators(n.committee, msg)
}

func (n *committeeNetwork) Subscribe(spectypes.ValidatorPK) error   { return nil }
func (n *committeeNetwork) Unsubscribe(spectypes.ValidatorPK) error { return nil }

func (n *committeeNetwork) ReportValidation(*spectypes.SSVMessage, protocolp2p.MsgValidationResult) {}
