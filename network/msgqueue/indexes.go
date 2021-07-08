package msgqueue

import (
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
)

// IBFTMessageIndexKey is the ibft index key
func IBFTMessageIndexKey(lambda []byte, seqNumber uint64, round uint64) string {
	return fmt.Sprintf("lambda_%s_seqNumber_%d_round_%d", hex.EncodeToString(lambda), seqNumber, round)
}

func iBFTMessageIndex() IndexFunc {
	return func(msg *network.Message) []string {
		if msg.Type != network.NetworkMsg_IBFTType {
			return []string{}
		}
		if msg.SignedMessage == nil || msg.SignedMessage.Message == nil {
			return []string{}
		}
		if msg.SignedMessage.Message.Lambda == nil {
			return []string{}
		}

		return []string{
			IBFTMessageIndexKey(msg.SignedMessage.Message.Lambda, msg.SignedMessage.Message.SeqNumber, msg.SignedMessage.Message.Round),
		}
	}
}

// IBFTAllRoundChangeIndexKey is the ibft index key for all round change msgs
func IBFTAllRoundChangeIndexKey(lambda []byte, seqNumber uint64) string {
	return fmt.Sprintf("lambda_%s_seqNumber_%d_all_round_changes", hex.EncodeToString(lambda), seqNumber)
}
func iBFTAllRoundChangeIndex() IndexFunc {
	return func(msg *network.Message) []string {
		if msg.Type == network.NetworkMsg_IBFTType &&
			msg.SignedMessage != nil &&
			msg.SignedMessage.Message != nil &&
			msg.SignedMessage.Message.Type == proto.RoundState_ChangeRound {
			return []string{
				IBFTAllRoundChangeIndexKey(msg.Lambda, msg.SignedMessage.Message.SeqNumber),
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
		if msg.Type != network.NetworkMsg_SignatureType {
			return []string{}
		}
		if msg.SignedMessage == nil || msg.SignedMessage.Message == nil {
			return []string{}
		}
		if msg.SignedMessage.Message.Lambda == nil {
			return []string{}
		}

		return []string{
			SigRoundIndexKey(msg.SignedMessage.Message.Lambda, msg.SignedMessage.Message.SeqNumber),
		}
	}
}

// DecidedIndexKey is the ibft decisions index key
func DecidedIndexKey(lambda []byte) string {
	return fmt.Sprintf("decided_lambda_%s", hex.EncodeToString(lambda))
}
func decidedMessageIndex() IndexFunc {
	return func(msg *network.Message) []string {
		if msg.Type != network.NetworkMsg_DecidedType {
			return []string{}
		}
		if msg.SignedMessage == nil || msg.SignedMessage.Message == nil {
			return []string{}
		}
		if msg.SignedMessage.Message.Lambda == nil {
			return []string{}
		}

		return []string{
			DecidedIndexKey(msg.SignedMessage.Message.Lambda),
		}
	}
}

// SyncIndexKey is the ibft sync index key
func SyncIndexKey(lambda []byte) string {
	return fmt.Sprintf("sync_lambda_%s", hex.EncodeToString(lambda))
}
func syncMessageIndex() IndexFunc {
	return func(msg *network.Message) []string {
		if msg.Type != network.NetworkMsg_SyncType {
			return []string{}
		}
		if msg.SyncMessage == nil {
			return []string{}
		}
		if msg.SyncMessage.Lambda == nil {
			return []string{}
		}

		return []string{
			SyncIndexKey(msg.SyncMessage.Lambda),
		}
	}
}
