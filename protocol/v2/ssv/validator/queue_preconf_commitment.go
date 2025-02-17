package validator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"go.uber.org/zap"
)

// HandleMessage handles a spectypes.SSVMessage.
// TODO: accept DecodedSSVMessage once p2p is upgraded to decode messages during validation.
// TODO: get rid of logger, add context
func (pc *PreconfCommitment) HandleMessage(_ context.Context, logger *zap.Logger, msg *queue.SSVMessage) {
	// logger.Debug("📬 handling SSV message",
	// 	zap.Uint64("type", uint64(msg.MsgType)),
	// 	fields.Role(msg.MsgID.GetRoleType()))

	slot, err := msg.Slot()
	if err != nil {
		logger.Error("❌ could not get slot from message", fields.MessageID(msg.MsgID), zap.Error(err))
		return
	}

	q := pc.prepareQueue(logger, slot)

	if pushed := q.Q.TryPush(msg); !pushed {
		msgID := msg.MsgID.String()
		logger.Warn("❗ dropping message because the queue is full",
			zap.String("msg_type", message.MsgTypeToString(msg.MsgType)),
			zap.String("msg_id", msgID))
	} else {
		// logger.Debug("📬 queue: pushed message", fields.MessageID(msg.MsgID), fields.MessageType(msg.MsgType))
	}
}

func (pc *PreconfCommitment) prepareQueue(logger *zap.Logger, slot phase0.Slot) queueContainer {
	pc.mtx.Lock()
	defer pc.mtx.Unlock()

	q, ok := pc.Queues[slot]
	if !ok {
		q = queueContainer{
			Q: queue.New(1000), // TODO alan: get queue opts from options
			queueState: &queue.State{
				HasRunningInstance: false,
				Height:             specqbft.Height(slot),
				Slot:               slot,
				//Quorum:             options.SSVShare.Share,// TODO
			},
		}
		c.Queues[slot] = q
		logger.Debug("missing queue for slot created", fields.Slot(slot))
	}

	return q
}

func (pc *PreconfCommitment) StartConsumeQueue(logger *zap.Logger, duty *spectypes.PreconfCommitmentDuty) error {
	pc.mtx.Lock()
	defer pc.mtx.Unlock()

	// Setting the cancel function separately due the queue could be created in HandleMessage
	q, found := pc.Queues[duty.ID]
	if !found {
		return fmt.Errorf("preconf-commitment, no queue found for ID %d", duty.ID)
	}

	r := pc.Runners[duty.ID]
	if r == nil {
		return fmt.Errorf("preconf-commitment, no runner found for ID %d", duty.ID)
	}

	// TODO - how long do we keep queue around (and what about runner)
	// required to stop the queue consumer when timeout message is received by handler
	queueCtx, cancelF := context.WithTimeout(pc.ctx, 32*12*time.Second)

	go func() {
		defer cancelF()
		if err := pc.ConsumeQueue(queueCtx, q, logger, pc.ProcessMessage, r); err != nil {
			logger.Error("❗failed consuming preconf-commitment queue", zap.Error(err))
		}
	}()
	return nil
}

// ConsumeQueue consumes messages from the queue.Queue of the controller
// it checks for current state
func (pc *PreconfCommitment) ConsumeQueue(
	ctx context.Context,
	q queueContainer,
	logger *zap.Logger,
	handler MessageHandler,
	rnr *runner.PreconfCommitmentRunner,
) error {
	state := *q.queueState

	logger.Debug("📬 queue consumer is running")
	lens := make([]int, 0, 10)

	for ctx.Err() == nil {
		// Construct a representation of the current state.
		var runningInstance *instance.Instance
		if rnr.HasRunningDuty() {
			runningInstance = rnr.GetBaseRunner().State.RunningInstance
			if runningInstance != nil {
				decided, _ := runningInstance.IsDecided()
				state.HasRunningInstance = !decided
			}
		}

		// Pop the highest priority message for the current state.
		// TODO: we don't really need NewCommitteeQueuePrioritizer here - looks like priority
		// makes sense only for qbft (Consensus) messages ?
		msg := q.Q.Pop(ctx, queue.NewCommitteeQueuePrioritizer(&state), queue.FilterAny)
		if ctx.Err() != nil {
			break
		}
		if msg == nil {
			logger.Error("❗ got nil message from queue, but context is not done!")
			break
		}
		lens = append(lens, q.Q.Len())
		if len(lens) >= 10 {
			logger.Debug("📬 [TEMPORARY] queue statistics",
				fields.MessageID(msg.MsgID), fields.MessageType(msg.MsgType),
				zap.Ints("past_10_lengths", lens))
			lens = lens[:0]
		}

		// Handle the message.
		if err := handler(ctx, logger, msg); err != nil {
			pc.logMsg(logger, msg, "❗ could not handle message",
				fields.MessageType(msg.SSVMessage.MsgType),
				zap.Error(err))
			if errors.Is(err, runner.ErrNoValidDuties) {
				// Stop the queue consumer if the runner no longer has any valid duties.
				break
			}
		}
	}

	logger.Debug("📪 queue consumer is closed")
	return nil
}

func (pc *PreconfCommitment) logMsg(logger *zap.Logger, msg *queue.SSVMessage, logMsg string, withFields ...zap.Field) {
	// can only be PartialSignatureMessages, must have been validated apriori
	psm := msg.Body.(*spectypes.PartialSignatureMessages)
	baseFields := []zap.Field{
		zap.Uint64("signer", psm.Messages[0].Signer),
		fields.Slot(psm.Slot),
	}
	logger.Debug(logMsg, append(baseFields, withFields...)...)
}
