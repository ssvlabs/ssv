package runner

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
)

// Compacts the given message's associated instance if it's either...
//   - Got a decided: to discard messages that are no longer needed. (proposes, prepares and sometimes commits)
//   - Advanced a round: to discard messages from previous rounds. (otherwise it might grow indefinitely)
func (b *BaseRunner) compactInstanceIfNeeded(msg *spectypes.SignedSSVMessage) {
	if inst := b.QBFTController.StoredInstances.FindInstance(msg.SSVMessage.Height); inst != nil {
		if controller.IsDecidedMsg(b.Share, msg) || specqbft.MessageType(msg.SSVMessage.MsgType) == specqbft.RoundChangeMsgType {
			instance.Compact(inst.State, msg)
		}
	}
}
