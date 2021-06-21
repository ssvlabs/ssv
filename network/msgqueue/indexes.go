package msgqueue

import (
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/network"
)

// IBFTRoundIndexKey is the ibft index key
func IBFTRoundIndexKey(lambda []byte, round uint64, validatorPk []byte) string {
	return fmt.Sprintf("lambda_%s_round_%d_pubKey_%s",
		hex.EncodeToString(lambda), round, hex.EncodeToString(validatorPk))
}

func iBFTMessageIndex() IndexFunc {
	return func(msg *network.Message) []string {
		if msg.Type == network.NetworkMsg_IBFTType {
			return []string{
				IBFTRoundIndexKey(msg.Lambda, msg.SignedMessage.Message.Round, msg.SignedMessage.Message.ValidatorPk),
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
