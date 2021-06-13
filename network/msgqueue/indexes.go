package msgqueue

import (
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
)

// IBFTRoundIndexKey is the ibft index key
func IBFTRoundIndexKey(lambda []byte, round uint64) string {
	return fmt.Sprintf("lambda_%s_round_%d", hex.EncodeToString(lambda), round)
}
func iBFTMessageIndex() IndexFunc {
	return func(msg *network.Message) []string {
		if msg.Type == network.NetworkMsg_IBFTType {
			return []string{
				IBFTRoundIndexKey(msg.Lambda, msg.SignedMessage.Message.Round),
			}
		}
		return []string{}
	}
}

// IBFTAllRoundChangeIndexKey is the ibft index key for all round change msgs
func IBFTAllRoundChangeIndexKey(lambda []byte) string {
	return fmt.Sprintf("lambda_%s_all_round_changes", hex.EncodeToString(lambda))
}
func iBFTAllRoundChangeIndex() IndexFunc {
	return func(msg *network.Message) []string {
		if msg.Type == network.NetworkMsg_IBFTType &&
			msg.SignedMessage != nil &&
			msg.SignedMessage.Message != nil &&
			msg.SignedMessage.Message.Type == proto.RoundState_ChangeRound {
			return []string{
				IBFTAllRoundChangeIndexKey(msg.Lambda),
			}
		}
		return []string{}
	}
}

// SigRoundIndexKey is the SSV node signature collection index key
func SigRoundIndexKey(lambda []byte) string {
	return fmt.Sprintf("sig_lambda_%s", hex.EncodeToString(lambda))
}
func sigMessageIndex() IndexFunc {
	return func(msg *network.Message) []string {
		if msg.Type == network.NetworkMsg_SignatureType {
			return []string{
				SigRoundIndexKey(msg.Lambda),
			}
		}
		return []string{}
	}
}
