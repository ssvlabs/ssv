package validator

import (
	"context"
	"fmt"
	"strings"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
)

// MessageHandler process the provided message. Message processing can fail with retryable or
// non-retryable error (can be checked via `runner.IsRetryable(err)`).
type MessageHandler func(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage) error

type msgIDType string

// messageID returns an ID that represents a potentially retryable message (msg.ID is the same for messages
// with different signers, slots, types, rounds, etc. - so we can't use just msg.ID as a unique identifier)
func messageID(msg *queue.SSVMessage, logger *zap.Logger) msgIDType {
	const idUndefined = "undefined"
	msgSlot, err := msg.Slot()
	if err != nil {
		logger.Error("couldn't get message slot", zap.Error(err))
		return idUndefined
	}
	if msg.MsgType == spectypes.SSVConsensusMsgType {
		sm := msg.Body.(*specqbft.Message)
		signers := strings.Join(strings.Fields(fmt.Sprint(msg.SignedSSVMessage.OperatorIDs)), "-")
		return msgIDType(fmt.Sprintf("%d-%d-%d-%d-%s-%s", msgSlot, msg.MsgType, sm.MsgType, sm.Round, msg.MsgID, signers))
	}
	if msg.MsgType == spectypes.SSVPartialSignatureMsgType {
		psm := msg.Body.(*spectypes.PartialSignatureMessages)
		signer := fmt.Sprintf("%d", psm.Messages[0].Signer) // same signer for all messages
		return msgIDType(fmt.Sprintf("%d-%d-%d-%s-%s", msgSlot, msg.MsgType, psm.Type, msg.MsgID, signer))
	}
	return idUndefined
}

func logWithMessageMetadata(logger *zap.Logger, msg *queue.SSVMessage) *zap.Logger {
	logger = logger.With(fields.MessageType(msg.MsgType))

	if msg.MsgType == spectypes.SSVConsensusMsgType {
		qbftMsg := msg.Body.(*specqbft.Message)
		logger = logger.With(
			zap.Uint64("consensus_msg_type", uint64(qbftMsg.MsgType)),
			zap.Any("signers", msg.SignedSSVMessage.OperatorIDs),
		)
	}

	if msg.MsgType == spectypes.SSVPartialSignatureMsgType {
		psm := msg.Body.(*spectypes.PartialSignatureMessages)
		// signer must be the same for all messages, at least 1 message must be present (this is validated prior)
		signer := psm.Messages[0].Signer
		logger = logger.With(
			zap.Uint64("partial_sig_msg_type", uint64(psm.Type)),
			zap.Uint64("signer", signer),
		)
	}

	return logger
}
