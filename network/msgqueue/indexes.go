package msgqueue

import (
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/network"
)

// IBFTRoundIndexKey is the ibft index key
func IBFTRoundIndexKey(lambda []byte, seqNumber uint64, round uint64) string {
	return fmt.Sprintf("lambda_%s_seqNumber_%d_round_%d", hex.EncodeToString(lambda), seqNumber, round)
}

func iBFTMessageIndex() IndexFunc {
	return func(msg *network.Message) []string {
		if msg.Type == network.NetworkMsg_IBFTType {
			return []string{
				IBFTRoundIndexKey(msg.Lambda, msg.SignedMessage.Message.SeqNumber, msg.SignedMessage.Message.Round),
			}
		}
		return []string{}
	}
}

// SigRoundIndexKey is the SSV node signature collection index key
func SigRoundIndexKey(lambda []byte, seqNumber uint64) string {
	return fmt.Sprintf("sig_lambda_%s_seqNumber_%d", hex.EncodeToString(lambda), seqNumber)
}
func sigMessageIndex() IndexFunc {
	return func(msg *network.Message) []string {
		if msg.Type == network.NetworkMsg_SignatureType {
			return []string{
				SigRoundIndexKey(msg.Lambda, msg.SignedMessage.Message.SeqNumber),
			}
		}
		return []string{}
	}
}

// DecidedIndexKey is the ibft decisions index key
func DecidedIndexKey(lambda []byte) string {
	return fmt.Sprintf("decided_lambda_%s", hex.EncodeToString(lambda))
}
func decidedMessageIndex() IndexFunc {
	return func(msg *network.Message) []string {
		if msg.Type == network.NetworkMsg_DecidedType {
			return []string{
				DecidedIndexKey(msg.Lambda),
			}
		}
		return []string{}
	}
}

// SyncIndexKey is the ibft sync index key
func SyncIndexKey(pubKey []byte) string {
	return fmt.Sprintf("sync_publicKey_%s", hex.EncodeToString(pubKey))
}
func syncMessageIndex() IndexFunc {
	return func(msg *network.Message) []string {
		if msg.Type == network.NetworkMsg_SyncType {
			return []string{
				SyncIndexKey(msg.SyncMessage.ValidatorPk),
			}
		}
		return []string{}
	}
}
