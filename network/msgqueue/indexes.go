package msgqueue

import (
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/network"
)

// IBFTRoundIndexKey is the ibft index key
func IBFTRoundIndexKey(lambda []byte, round uint64) string {
	return fmt.Sprintf("lambda_%s_round_%d", hex.EncodeToString(lambda), round)
}
// IBFTRoundIndexKeyPK is the ibft index key
func IBFTRoundIndexKeyPK(lambda []byte, round uint64, validatorPk []byte) string {
	return fmt.Sprintf("lambda_%s_round_%d_pubKey_%s",
		hex.EncodeToString(lambda), round, hex.EncodeToString(validatorPk))
}
func iBFTMessageIndex() IndexFunc {
	return func(msg *network.Message) []string {
		if msg.Type == network.NetworkMsg_IBFTType {
			if len(msg.SignedMessage.Message.ValidatorPk) > 0 {
				return []string{
					IBFTRoundIndexKeyPK(msg.Lambda, msg.SignedMessage.Message.Round, msg.SignedMessage.Message.ValidatorPk),
				}
			}
			return []string{
				IBFTRoundIndexKey(msg.Lambda, msg.SignedMessage.Message.Round),
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
