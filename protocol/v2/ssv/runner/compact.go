package runner

import (
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
)

// Compacts the given message's associated instance if it's either...
//   - Got a decided: to discard messages that are no longer needed. (proposes, prepares and sometimes commits)
//   - Advanced a round: to discard messages from previous rounds. (otherwise it might grow indefinitely)
func (b *BaseRunner) compactInstanceIfNeeded(msg *specqbft.SignedMessage) {
	if inst := b.QBFTController.StoredInstances.FindInstance(msg.Message.Height); inst != nil {
		if controller.IsDecidedMsg(b.Share, msg) || msg.Message.MsgType == specqbft.RoundChangeMsgType {
			instance.Compact(inst.State, msg)
		}
	}
}
