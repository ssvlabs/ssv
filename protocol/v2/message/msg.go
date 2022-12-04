package message

import spectypes "github.com/bloxapp/ssv-spec/types"

const (
	// SSVDecidedMsgType extends spec msg type
	SSVDecidedMsgType spectypes.MsgType = 3
	// SSVSyncMsgType extends spec msg type
	SSVSyncMsgType spectypes.MsgType = 4
)

// MsgTypeToString extension for spec msg type. convert spec msg type to string
func MsgTypeToString(mt spectypes.MsgType) string {
	switch mt {
	case spectypes.SSVConsensusMsgType:
		return "consensus"
	case SSVDecidedMsgType:
		return "decided"
	case spectypes.SSVPartialSignatureMsgType:
		return "partialSignature"
	case spectypes.DKGMsgType:
		return "dkg"
	case SSVSyncMsgType:
		return "sync"
	default:
		return ""
	}
}
