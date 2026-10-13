package testing

import (
	"crypto/rsa"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"

	protocolp2p "github.com/ssvlabs/ssv/protocol/v2/p2p"
)

// TestingNetwork wraps the spec testing network to satisfy protocolp2p.Network, which adds
// BroadcastAtSlot (needed for Boole-fork topic gating) on top of the spec's base Network.
type TestingNetwork struct {
	*spectestingutils.TestingNetwork
}

var _ protocolp2p.Network = (*TestingNetwork)(nil)

func NewTestingNetwork(operatorID spectypes.OperatorID, sk *rsa.PrivateKey) *TestingNetwork {
	return &TestingNetwork{TestingNetwork: spectestingutils.NewTestingNetwork(operatorID, sk)}
}

func (n *TestingNetwork) Subscribe(_ spectypes.ValidatorPK) error {
	return nil
}

func (n *TestingNetwork) Unsubscribe(_ spectypes.ValidatorPK) error {
	return nil
}

func (n *TestingNetwork) BroadcastAtSlot(message *spectypes.SignedSSVMessage, _ phase0.Slot) error {
	return n.Broadcast(message.SSVMessage.GetID(), message)
}

func (n *TestingNetwork) ReportValidation(_ *spectypes.SSVMessage, _ protocolp2p.MsgValidationResult) {
}
