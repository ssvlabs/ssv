package runner

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

// Compacts the given message's associated instance if it's either...
//   - Got a decided: to discard messages that are no longer needed. (proposes, prepares and sometimes commits)
//   - Advanced a round: to discard messages from previous rounds. (otherwise it might grow indefinitely)
func (b *BaseRunner) compactInstanceIfNeeded(msg ssvtypes.SignedMessage) {
	if inst := b.QBFTController.StoredInstances.FindInstance(msg.GetHeight()); inst != nil {
		if controller.IsDecidedMsg(b.GetShare(), msg) || msg.GetType() == specqbft.RoundChangeMsgType {
			instance.Compact(inst.State, msg)
		}
	}
}
