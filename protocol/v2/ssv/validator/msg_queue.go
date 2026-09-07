package validator

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jellydator/ttlcache/v3"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/protocol/v2/message"
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

// messageStateTTL bounds how long a message's processing state outlives its last attempt: long enough
// for a retried message parked in the queue to keep its retry count and span across a realistic wait,
// short enough that the cache does not grow without bound.
const messageStateTTL = 10 * time.Minute

// newMessageStates returns the per-message processing-state cache the queue consumers share. A state
// normally ends its span when the message is processed or dropped; one that expires instead — a retried
// message still parked in the queue — ends it here, so the span is exported rather than lost.
func newMessageStates(ttl time.Duration) *ttlcache.Cache[messageKey, *messageProcessingState] {
	states := ttlcache.New(ttlcache.WithTTL[messageKey, *messageProcessingState](ttl))
	states.OnEviction(func(_ context.Context, reason ttlcache.EvictionReason, item *ttlcache.Item[messageKey, *messageProcessingState]) {
		if reason == ttlcache.EvictionReasonExpired {
			item.Value().span.End()
		}
	})
	return states
}

const maxInt64DecimalLen = 20 // enough for uint64 max or int64 min in base 10

func writeUint64(b *strings.Builder, v uint64) {
	var buf [maxInt64DecimalLen]byte
	out := strconv.AppendUint(buf[:0], v, 10)
	_, _ = b.Write(out)
}

func writeInt64(b *strings.Builder, v int64) {
	var buf [maxInt64DecimalLen]byte
	out := strconv.AppendInt(buf[:0], v, 10)
	_, _ = b.Write(out)
}

func writeMsgIDHex(b *strings.Builder, id spectypes.MessageID) {
	// MessageID.String() allocates; encoding directly avoids that.
	const msgIDHexLen = len(spectypes.MessageID{}) * 2
	var buf [msgIDHexLen]byte
	hex.Encode(buf[:], id[:])
	_, _ = b.Write(buf[:])
}

func writeOperatorIDs(b *strings.Builder, operatorIDs []spectypes.OperatorID) {
	b.WriteByte('[')
	for i, operatorID := range operatorIDs {
		if i > 0 {
			b.WriteByte('-')
		}
		writeUint64(b, operatorID)
	}
	b.WriteByte(']')
}

// mKey returns an ID that represents a potentially retryable message (msg.ID is the same for messages
// with different signers, slots, types, rounds, etc. - so we can't use just msg.ID as a unique identifier)
func mKey(msg *queue.SSVMessage) (messageKey, error) {
	msgSlot, err := msg.Slot()
	if err != nil {
		return "", fmt.Errorf("couldn't get message slot: %w", err)
	}

	if msg.MsgType == message.SSVEventMsgType {
		eventMsg, ok := msg.Body.(*ssvtypes.EventMsg)
		if !ok || eventMsg == nil {
			return "", fmt.Errorf("event message: invalid msg body, type: %T", msg.Body)
		}

		round := uint64(0)
		if eventMsg.Type == ssvtypes.Timeout {
			timeoutData, err := eventMsg.GetTimeoutData()
			if err != nil {
				return "", fmt.Errorf("event message: get timeout data: %w", err)
			}
			round = uint64(timeoutData.Round)
		}
		var b strings.Builder
		b.Grow(200)
		writeUint64(&b, uint64(msgSlot))
		b.WriteByte('-')
		writeUint64(&b, uint64(msg.MsgType))
		b.WriteByte('-')
		writeInt64(&b, int64(eventMsg.Type))
		b.WriteByte('-')
		writeUint64(&b, round)
		b.WriteByte('-')
		writeMsgIDHex(&b, msg.MsgID)
		return messageKey(b.String()), nil
	}
	if msg.MsgType == spectypes.SSVConsensusMsgType {
		sm, ok := msg.Body.(*specqbft.Message)
		if !ok || sm == nil {
			return "", fmt.Errorf("qbft message: invalid msg body, type: %T", msg.Body)
		}
		var b strings.Builder
		b.Grow(224)
		writeUint64(&b, uint64(msgSlot))
		b.WriteByte('-')
		writeUint64(&b, uint64(msg.MsgType))
		b.WriteByte('-')
		writeUint64(&b, uint64(sm.MsgType))
		b.WriteByte('-')
		writeUint64(&b, uint64(sm.Round))
		b.WriteByte('-')
		writeMsgIDHex(&b, msg.MsgID)
		b.WriteByte('-')
		writeOperatorIDs(&b, msg.SignedSSVMessage.OperatorIDs)
		return messageKey(b.String()), nil
	}
	if msg.MsgType == spectypes.SSVPartialSignatureMsgType {
		psm, ok := msg.Body.(*spectypes.PartialSignatureMessages)
		if !ok || psm == nil {
			return "", fmt.Errorf("partial-sig message: invalid msg body, type: %T", msg.Body)
		}
		var b strings.Builder
		b.Grow(200)
		writeUint64(&b, uint64(msgSlot))
		b.WriteByte('-')
		writeUint64(&b, uint64(msg.MsgType))
		b.WriteByte('-')
		writeUint64(&b, uint64(psm.Type))
		b.WriteByte('-')
		writeMsgIDHex(&b, msg.MsgID)
		b.WriteByte('-')
		// same signer for all messages
		writeUint64(&b, ssvtypes.PartialSigMsgSigner(psm))
		return messageKey(b.String()), nil
	}
	if msg.MsgType == spectypes.SSVEnvelopeDisseminationMsgType {
		dissemination, ok := msg.Body.(*spectypes.EnvelopeDissemination)
		if !ok || dissemination == nil {
			return "", fmt.Errorf("envelope dissemination: invalid msg body, type: %T", msg.Body)
		}
		var b strings.Builder
		b.Grow(200)
		writeUint64(&b, uint64(msgSlot))
		b.WriteByte('-')
		writeUint64(&b, uint64(msg.MsgType))
		b.WriteByte('-')
		writeMsgIDHex(&b, msg.MsgID)
		b.WriteByte('-')
		// one dissemination per signer and slot (SIP #94 §7)
		writeOperatorIDs(&b, msg.SignedSSVMessage.OperatorIDs)
		return messageKey(b.String()), nil
	}

	return "", fmt.Errorf("unexpected message type (expected types: event, qbft, partial-sig, envelope-dissemination): %d", msg.MsgType)
}

func logWithMessageMetadata(logger *zap.Logger, msg *queue.SSVMessage) *zap.Logger {
	logger = logger.With(fields.MessageType(msg.MsgType))

	if msg.MsgType == spectypes.SSVConsensusMsgType {
		qbftMsg, ok := msg.Body.(*specqbft.Message)
		if !ok || qbftMsg == nil {
			logger.Error("logWithMessageMetadata: invalid qbft msg body", zap.String("type", fmt.Sprintf("%T", msg.Body)))
			return logger
		}
		logger = logger.With(
			zap.Uint64("consensus_msg_type", uint64(qbftMsg.MsgType)),
			zap.Any("signers", msg.SignedSSVMessage.OperatorIDs),
		)
		return logger
	}

	if msg.MsgType == spectypes.SSVPartialSignatureMsgType {
		psm, ok := msg.Body.(*spectypes.PartialSignatureMessages)
		if !ok || psm == nil {
			logger.Error("logWithMessageMetadata: invalid partial-sig msg body", zap.String("type", fmt.Sprintf("%T", msg.Body)))
			return logger
		}
		logger = logger.With(
			zap.Uint64("partial_sig_msg_type", uint64(psm.Type)),
			zap.Uint64("signer", ssvtypes.PartialSigMsgSigner(psm)),
		)
		return logger
	}

	if msg.MsgType == spectypes.SSVEnvelopeDisseminationMsgType {
		logger = logger.With(zap.Any("signers", msg.SignedSSVMessage.OperatorIDs))
		return logger
	}

	return logger
}
