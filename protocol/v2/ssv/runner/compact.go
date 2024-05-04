package runner

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"golang.org/x/exp/maps"

	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
)

// Compacts the given message's associated instance if it's either...
//   - Got a decided: to discard messages that are no longer needed. (proposes, prepares and sometimes commits)
//   - Advanced a round: to discard messages from previous rounds. (otherwise it might grow indefinitely)
func (b *BaseRunner) compactInstanceIfNeeded(signedMsg *spectypes.SignedSSVMessage) error {
	msg, err := specqbft.DecodeMessage(signedMsg.SSVMessage.Data)
	if err != nil {
		return err
	}
	if inst := b.QBFTController.StoredInstances.FindInstance(msg.Height); inst != nil {
		// TODO: rewrite this workaround
		firstShare := b.ShareMap[maps.Keys(b.ShareMap)[0]]
		decidedMsg, err := controller.IsDecidedMsg(firstShare, signedMsg)
		if err != nil {
			return err
		}

		if decidedMsg || msg.MsgType == specqbft.RoundChangeMsgType {
			instance.Compact(inst.State, signedMsg)
		}
	}

	return nil
}
