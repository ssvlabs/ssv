package validator

import (
	"context"
	"fmt"
	"strings"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// MessageHandler process the provided message. Message processing can fail with retryable or
// non-retryable error (can be checked via `runner.IsRetryable(err)`).
type MessageHandler func(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage) error

// messageProcessingState tracks retries and span context for a specific message.
type messageProcessingState struct {
	// attempts is how many attempts have already been tried for this message.
	attempts int64

	// ctx is stored here so we can use it to derive child-spans for span.
	ctx context.Context
	// span related to this p2p message, it tracks accumulates the message-related events.
	span trace.Span
}

type messageKey string

// mKey returns an ID that represents a potentially retryable message (msg.ID is the same for messages
// with different signers, slots, types, rounds, etc. - so we can't use just msg.ID as a unique identifier)
func mKey(msg *queue.SSVMessage, logger *zap.Logger) messageKey {
	const idUndefined = "undefined"
	msgSlot, err := msg.Slot()
	if err != nil {
		logger.Error("couldn't get message slot", zap.Error(err))
		return idUndefined
	}
	if msg.MsgType == spectypes.SSVConsensusMsgType {
		sm := msg.Body.(*specqbft.Message)
		signers := strings.Join(strings.Fields(fmt.Sprint(msg.SignedSSVMessage.OperatorIDs)), "-")
		return messageKey(fmt.Sprintf("%d-%d-%d-%d-%s-%s", msgSlot, msg.MsgType, sm.MsgType, sm.Round, msg.MsgID, signers))
	}
	if msg.MsgType == spectypes.SSVPartialSignatureMsgType {
		psm := msg.Body.(*spectypes.PartialSignatureMessages)
		signer := fmt.Sprintf("%d", ssvtypes.PartialSigMsgSigner(psm)) // same signer for all messages
		return messageKey(fmt.Sprintf("%d-%d-%d-%s-%s", msgSlot, msg.MsgType, psm.Type, msg.MsgID, signer))
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
		logger = logger.With(
			zap.Uint64("partial_sig_msg_type", uint64(psm.Type)),
			zap.Uint64("signer", ssvtypes.PartialSigMsgSigner(psm)),
		)
	}

	return logger
}
