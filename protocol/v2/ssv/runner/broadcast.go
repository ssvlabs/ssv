package runner

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

type broadcasterAtSlot interface {
	BroadcastAtSlot(message *spectypes.SignedSSVMessage, slot phase0.Slot) error
}

func broadcastAtSlot(net specqbft.Network, msg *spectypes.SignedSSVMessage, slot phase0.Slot) error {
	if broadcaster, ok := net.(broadcasterAtSlot); ok {
		return broadcaster.BroadcastAtSlot(msg, slot)
	}
	return net.Broadcast(msg.SSVMessage.GetID(), msg)
}
