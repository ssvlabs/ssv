package runner

import (
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// Compacts the given message's associated instance if it's either...
//   - Got a decided: to discard messages that are no longer needed. (proposes, prepares and sometimes commits)
//   - Advanced a round: to discard messages from previous rounds. (otherwise it might grow indefinitely)
func (b *BaseRunner) compactInstanceIfNeeded(signedMsg *spectypes.SignedSSVMessage) {
	//
	//msg, err := specqbft.DecodeMessage(signedMsg.SSVMessage.Data)
	//if err != nil {
	//
	//}
	//
	////if inst := b.QBFTController.StoredInstances.FindInstance(msg.Height); inst != nil {
	////	if controller.IsDecidedMsg(b.Share[signedMsg.OperatorIDs.], msg) || msg.MsgType == specqbft.RoundChangeMsgType {
	////		instance.Compact(inst.State, msg)
	////	}
	////}
}
