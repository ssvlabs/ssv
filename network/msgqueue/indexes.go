package msgqueue

import (
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/network"
)

func IBFTRoundIndexKey(lambda []byte, round uint64) string {
	return fmt.Sprintf("lambda_%s_round_%d", hex.EncodeToString(lambda), round)
}
func iBFTMessageIndex() IndexFunc {
	return func(msg *network.Message) []string {
		return []string{
			IBFTRoundIndexKey(msg.Lambda, msg.Msg.Message.Round),
		}
	}
}

func SigRoundIndexKey(lambda []byte) string {
	return fmt.Sprintf("sig_lambda_%s", hex.EncodeToString(lambda))
}
func sigMessageIndex() IndexFunc {
	return func(msg *network.Message) []string {
		return []string{
			SigRoundIndexKey(msg.Lambda),
		}
	}
}
