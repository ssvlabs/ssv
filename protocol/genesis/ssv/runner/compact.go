package runner

import (
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	"github.com/ssvlabs/ssv/protocol/genesis/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/genesis/qbft/instance"
)

// Compacts the given message's associated instance if it's either...
//   - Got a decided: to discard messages that are no longer needed. (proposes, prepares and sometimes commits)
//   - Advanced a round: to discard messages from previous rounds. (otherwise it might grow indefinitely)
func (b *BaseRunner) compactInstanceIfNeeded(msg *genesisspecqbft.SignedMessage) {
	if inst := b.QBFTController.StoredInstances.FindInstance(msg.Message.Height); inst != nil {
		if controller.IsDecidedMsg(b.Share, msg) || msg.Message.MsgType == genesisspecqbft.RoundChangeMsgType {
			instance.Compact(inst.State, msg)
		}
	}
}
