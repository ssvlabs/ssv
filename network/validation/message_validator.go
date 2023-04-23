package validation

import (
	"context"
	"fmt"
	"runtime"

	"github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/network/forks"
	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v2/types"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type ShareLookupFunc func(validatorPK spectypes.ValidatorPK) (*types.SSVShare, error)

type MessageValidator interface {
	ValidateMessage(ctx context.Context, peerID peer.ID, msg *pubsub.Message) Result
}

type messageValidator struct {
	logger   *zap.Logger
	fork     forks.Fork
	getShare ShareLookupFunc
	schedule *messageSchedule
	verifier *signatureVerifier
}

func NewMessageValidator(ctx context.Context, logger *zap.Logger, fork forks.Fork, getShare ShareLookupFunc) MessageValidator {
	return &messageValidator{
		logger:   logger,
		fork:     fork,
		getShare: getShare,
		schedule: newMessageSchedule(),
		verifier: newSignatureVerifier(ctx, logger, runtime.NumCPU()),
	}
}

// ValidateMessage validates the given message.
func (v *messageValidator) ValidateMessage(ctx context.Context, peerID peer.ID, msg *pubsub.Message) Result {
	iv := &individualMessageValidator{
		messageValidator: v,
		peerID:           peerID,
		msg:              msg,
	}
	if err := iv.validate(ctx); err != nil {
		v.logger.Debug("validation failed", zap.Error(err))

		if result, ok := err.(Result); ok {
			return result
		}
		return Result{Action: pubsub.ValidationReject, Reason: ReasonError, Err: err}
	}
	return Result{Action: pubsub.ValidationAccept}
}

type individualMessageValidator struct {
	*messageValidator
	peerID peer.ID
	msg    *pubsub.Message
}

func (v *individualMessageValidator) validate(ctx context.Context) error {
	topic := v.msg.GetTopic()
	logger := v.logger.With(fields.PeerID(v.peerID), fields.Topic(topic))
	logger.Debug("validating message")

	// TODO: cache metrics per topic to avoid repeated WithLabelValues calls?
	metricPubsubActiveMsgValidation.WithLabelValues(topic).Inc()
	defer metricPubsubActiveMsgValidation.WithLabelValues(topic).Dec()

	// Decode SSV message.
	if len(v.msg.GetData()) == 0 {
		return reject(ReasonEmptyData)
	}
	msg, err := v.fork.DecodeNetworkMsg(v.msg.GetData())
	if err != nil {
		return reject(ReasonMalformed, err)
	}
	if msg == nil {
		return reject(ReasonMalformed, errors.New("decoded message is nil"))
	}

	// Preserve for later use.
	v.msg.ValidatorData = *msg

	// Validate SSV message.
	return v.validateSSVMessage(ctx, logger, msg)
}

func (v *individualMessageValidator) validateSSVMessage(ctx context.Context, logger *zap.Logger, msg *spectypes.SSVMessage) error {
	logger = logger.With(fields.PubKey(msg.MsgID.GetPubKey()), fields.Role(msg.MsgID.GetRoleType()))
	switch msg.MsgType {
	case spectypes.SSVConsensusMsgType:
		share, err := v.getShare(msg.MsgID.GetPubKey())
		if err != nil || share == nil {
			return reject(ReasonValidatorNotFound, err)
		}
		signedMsg := qbft.SignedMessage{}
		err = signedMsg.Decode(msg.GetData())
		if err != nil {
			return reject(ReasonMalformed, errors.Wrap(err, "failed to decode QBFT message"))
		}
		return v.validateConsensusMsg(ctx, logger, &signedMsg, share)

	default:
		// TODO: validate the other types!
		return nil
	}
}

/*
Main controller processing flow

All decided msgs are processed the same, out of instance
All valid future msgs are saved in a container and can trigger the highest decided future msg
All other msgs (not future or decided) are processed normally by an existing instance (if found)
*/
func (v *individualMessageValidator) validateConsensusMsg(ctx context.Context, logger *zap.Logger, signedMsg *qbft.SignedMessage, share *types.SSVShare) error {
	logger = logger.With(zap.Any("msgType", signedMsg.Message.MsgType), zap.Any("msgRound", signedMsg.Message.Round),
		zap.Any("msgHeight", signedMsg.Message.Height), zap.Any("signers", signedMsg.Signers))

	logger.Info("validating consensus message")

	// syntactic check
	if signedMsg.Validate() != nil {
		return reject(ReasonSyntacticError)
	}

	// TODO can this be inside another syntactic check? or maybe we can move more syntactic checks here that happen later on?
	if len(signedMsg.Signers) > len(share.Committee) {
		return reject(ReasonSyntacticError, errors.New("too many signers"))
	}

	// if isDecided msg (this propagates to all topics)
	if controller.IsDecidedMsg(&share.Share, signedMsg) {
		return v.validateDecidedMessage(ctx, logger, signedMsg, share)
	}

	// if non-decided msg and I am a committee-validator
	// is message is timely?
	// if no instance of qbft
	// base validation
	// sig validation
	// mark message

	// if qbft instance is decided (I am a validator)
	// base commit message validation
	// sig commit message validation
	// is commit msg aggratable
	// mark message

	// If qbft instance is not decided (I am a validator)
	// Full validation of messages
	//mark consensus message

	return v.validateQbftMessage(ctx, logger, signedMsg, share)
}

func (v *individualMessageValidator) validateQbftMessage(ctx context.Context, logger *zap.Logger, msg *qbft.SignedMessage, share *types.SSVShare) error {
	logger.Info("validating qbft message")
	markLockID := fmt.Sprintf("%#x-%d", msg.Message.Identifier, msg.Signers[0])

	// If we don't lock we may have a race between findMark and markConsensusMsg
	v.schedule.sto.Lock(markLockID)
	defer v.schedule.sto.Unlock(markLockID)

	sm, found := v.schedule.marks.GetOrCreateSignerMark(messageID(msg.Message.Identifier), msg.Signers[0])
	if found {
		logger = logger.With(zap.Any("markedRound", sm.HighestRound), zap.Any("markedHeight", sm.HighestDecided))
		minRoundTime := v.schedule.minFirstMsgTimeForRound(msg, sm.HighestRound, logger)

		isTimely, action := sm.isConsensusMsgTimely(msg, logger, minRoundTime)
		if !isTimely {
			return Result{
				Action: action,
				Reason: ReasonNotTimely,
			}
		}
		// TODO: fix case where a past height message is received
		if sm.tooManyMessagesPerRound(msg, share, logger) {
			return reject(ReasonTooManyMsgs, nil)
		}
	}
	//sig validation
	err := v.verifier.Verify(ctx, msg, share.DomainType, spectypes.QBFTSignatureType, share.Committee)
	if err != nil {
		return reject(ReasonInvalidSig, err)
	}

	// mark message
	v.schedule.MarkConsensusMessage(logger, sm, msg.Message.Identifier, msg.Signers[0], msg.Message.Round, msg.Message.MsgType)

	// base commit message validation
	// sig commit message validation
	// is commit msg aggratable
	// mark message
	return nil
}

func (v *individualMessageValidator) validateDecidedMessage(ctx context.Context, logger *zap.Logger, msg *qbft.SignedMessage, share *types.SSVShare) error {
	logger = logger.
		With(zap.String("msgType", "decided")).
		With(zap.Any("signers", msg.Signers)).
		With(zap.Any("height", msg.Message.Height))

	logger.Info("validate decided message")

	// TODO we don't need to lock the scheduler here, because even if validateQbftMessage creates a mark here it shouldn't affect the result
	v.schedule.sto.Lock(string(msg.Message.Identifier))
	defer v.schedule.sto.Unlock(string(msg.Message.Identifier))
	// this decision is made to reduce lock retention, but it may make the code more fragile to changes
	// when we create the mark we do lock the scheduler, so we are safe

	// check if better decided
	// Mainly to have better statistics on commitments with a full signer set
	// TODO can it cause liveness failure?
	// if a different message is not better, then theoretically a better message should be broadcasted by at least one node
	hasBetterOrSimilarMsg, action := v.schedule.hasBetterOrSimilarMsg(msg, share.Quorum, uint64(len(share.Committee)), v.peerID)
	if hasBetterOrSimilarMsg {
		return Result{
			Action: action,
			Reason: ReasonBetterMessage,
		}
	}

	//check if timely decided
	if !v.schedule.isTimelyDecidedMsg(msg, logger) {
		return reject(ReasonNotTimely, nil)
	}

	// validate decided message
	// TODO calls signedMsg.validate() again, this call does some useless stuff
	// TODO should do deeper syntax validation for commit data? Or eth protocol protects well enough?
	// IMO such validation matters less for DOS protection so we shouldn't spend too much time on it
	if err := controller.ValidateDecidedWithoutSignature(msg, &share.Share); err != nil {
		return reject(ReasonSyntacticError, err)
	}

	//sig validation
	err := v.verifier.Verify(ctx, msg, share.DomainType, spectypes.QBFTSignatureType, share.Committee)
	if err != nil {
		return reject(ReasonInvalidSig, err)
	}
	//mark decided message
	v.schedule.markDecidedMsg(msg, share, logger)
	return nil
}

// NewPubsubMessageValidator returns a pubsub-compatible validator for the given MessageValidator.
func NewPubsubMessageValidator(v MessageValidator) func(ctx context.Context, p peer.ID, msg *pubsub.Message) pubsub.ValidationResult {
	return func(ctx context.Context, p peer.ID, msg *pubsub.Message) pubsub.ValidationResult {
		result := v.ValidateMessage(ctx, p, msg)
		return result.Action
	}
}
