package validator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

var (
	// runnerExpirySlots - Committee messages are allowed up to 34 slots in the future. All runners that are older can be stopped.
	runnerExpirySlots = phase0.Slot(34)
)

type CommitteeRunnerFunc func(slot phase0.Slot, shares map[phase0.ValidatorIndex]*spectypes.Share, attestingValidators []spectypes.ShareValidatorPK, dutyGuard runner.CommitteeDutyGuard) (*runner.CommitteeRunner, error)

type Committee struct {
	logger *zap.Logger
	ctx    context.Context
	cancel context.CancelFunc

	mtx           sync.RWMutex
	BeaconNetwork spectypes.BeaconNetwork

	Queues  map[phase0.Slot]queueContainer
	Runners map[phase0.Slot]*runner.CommitteeRunner
	Shares  map[phase0.ValidatorIndex]*spectypes.Share

	CommitteeMember *spectypes.CommitteeMember

	dutyGuard      *CommitteeDutyGuard
	CreateRunnerFn CommitteeRunnerFunc
}

// NewCommittee creates a new cluster
func NewCommittee(
	ctx context.Context,
	cancel context.CancelFunc,
	logger *zap.Logger,
	beaconNetwork spectypes.BeaconNetwork,
	committeeMember *spectypes.CommitteeMember,
	createRunnerFn CommitteeRunnerFunc,
	shares map[phase0.ValidatorIndex]*spectypes.Share,
	dutyGuard *CommitteeDutyGuard,
) *Committee {
	if shares == nil {
		shares = make(map[phase0.ValidatorIndex]*spectypes.Share)
	}
	return &Committee{
		logger:          logger,
		BeaconNetwork:   beaconNetwork,
		ctx:             ctx,
		cancel:          cancel,
		Queues:          make(map[phase0.Slot]queueContainer),
		Runners:         make(map[phase0.Slot]*runner.CommitteeRunner),
		Shares:          shares,
		CommitteeMember: committeeMember,
		CreateRunnerFn:  createRunnerFn,
		dutyGuard:       dutyGuard,
	}
}

func (c *Committee) AddShare(share *spectypes.Share) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.Shares[share.ValidatorIndex] = share
}

func (c *Committee) RemoveShare(validatorIndex phase0.ValidatorIndex) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if share, exist := c.Shares[validatorIndex]; exist {
		c.dutyGuard.StopValidator(share.ValidatorPubKey)
		delete(c.Shares, validatorIndex)
	}
}

func (c *Committee) StartConsumeQueue(ctx context.Context, logger *zap.Logger, duty *spectypes.CommitteeDuty) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	ctx, span := tracer.Start(ctx,
		fmt.Sprintf("%s.start_consume_queue", observabilityNamespace),
		trace.WithAttributes(
			attribute.String("ssv.runner.role", duty.RunnerRole().String()),
			attribute.Int("ssv.validator.duty_count", len(duty.ValidatorDuties)),
			attribute.Int64("ssv.validator.duty.slot", int64(duty.Slot)),
		))
	defer span.End()

	// Setting the cancel function separately due the queue could be created in HandleMessage
	q, found := c.Queues[duty.Slot]
	if !found {
		err := errors.New(fmt.Sprintf("no queue found for slot %d", duty.Slot))
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	r := c.Runners[duty.Slot]
	if r == nil {
		err := errors.New(fmt.Sprintf("no runner found for slot %d", duty.Slot))
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	// required to stop the queue consumer when timeout message is received by handler
	queueCtx, cancelF := context.WithDeadline(ctx, time.Unix(c.BeaconNetwork.EstimatedTimeAtSlot(duty.Slot+runnerExpirySlots), 0))

	go func() {
		defer cancelF()
		if err := c.ConsumeQueue(queueCtx, q, logger, duty.Slot, c.ProcessMessage, r); err != nil {
			logger.Error("❗failed consuming committee queue", zap.Error(err))
			span.RecordError(err)
		}
	}()

	span.SetStatus(codes.Ok, "")
	return nil
}

// StartDuty starts a new duty for the given slot
func (c *Committee) StartDuty(ctx context.Context, logger *zap.Logger, duty *spectypes.CommitteeDuty) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	ctx, span := tracer.Start(ctx,
		fmt.Sprintf("%s.start_duty", observabilityNamespace),
		trace.WithAttributes(
			attribute.String("ssv.runner.role", duty.RunnerRole().String()),
			attribute.Int("ssv.validator.duty_count", len(duty.ValidatorDuties)),
			attribute.Int64("ssv.validator.duty.slot", int64(duty.Slot)),
		))
	defer span.End()

	if len(duty.ValidatorDuties) == 0 {
		err := errors.New("no beacon duties")
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if _, exists := c.Runners[duty.Slot]; exists {
		err := errors.New(fmt.Sprintf("CommitteeRunner for slot %d already exists", duty.Slot))
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	// Filter out Beacon duties for which we don't have a share.
	filteredDuty := &spectypes.CommitteeDuty{
		Slot:            duty.Slot,
		ValidatorDuties: make([]*spectypes.ValidatorDuty, 0, len(duty.ValidatorDuties)),
	}
	shares := make(map[phase0.ValidatorIndex]*spectypes.Share, len(duty.ValidatorDuties))
	attesters := make([]spectypes.ShareValidatorPK, 0, len(duty.ValidatorDuties))
	for _, beaconDuty := range duty.ValidatorDuties {
		share, exists := c.Shares[beaconDuty.ValidatorIndex]
		if !exists {
			span.AddEvent("no share for validator duty", trace.WithAttributes(
				attribute.Int64("ssv.validator.index", int64(beaconDuty.ValidatorIndex)),
				attribute.String("ssv.beacon.role", beaconDuty.Type.String()),
				attribute.String("ssv.validator.pubkey", beaconDuty.PubKey.String()),
			))
			continue
		}
		shares[beaconDuty.ValidatorIndex] = share
		filteredDuty.ValidatorDuties = append(filteredDuty.ValidatorDuties, beaconDuty)

		if beaconDuty.Type == spectypes.BNRoleAttester {
			attesters = append(attesters, share.SharePubKey)
		}
	}
	if len(shares) == 0 {
		err := errors.New("no shares for duty's validators")
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	duty = filteredDuty

	runner, err := c.CreateRunnerFn(duty.Slot, shares, attesters, c.dutyGuard)
	if err != nil {
		return errors.Wrap(err, "could not create CommitteeRunner")
	}

	// Set timeout function.
	runner.GetBaseRunner().TimeoutF = c.onTimeout
	c.Runners[duty.Slot] = runner
	_, queueExists := c.Queues[duty.Slot]
	if !queueExists {
		c.Queues[duty.Slot] = queueContainer{
			Q: queue.New(1000), // TODO alan: get queue opts from options
			queueState: &queue.State{
				HasRunningInstance: false,
				Height:             qbft.Height(duty.Slot),
				Slot:               duty.Slot,
				Quorum:             c.CommitteeMember.GetQuorum(),
			},
		}
	}

	// Prunes all expired committee runners, when new runner is created
	pruneLogger := c.logger.With(zap.Uint64("current_slot", uint64(duty.Slot)))
	if err := c.unsafePruneExpiredRunners(pruneLogger, duty.Slot); err != nil {
		span.RecordError(err)
		pruneLogger.Error("couldn't prune expired committee runners", zap.Error(err))
	}

	span.AddEvent("ℹ️ starting duty processing")
	err = runner.StartNewDuty(ctx, logger, duty, c.CommitteeMember.GetQuorum())
	if err != nil {
		err := errors.Wrap(err, "runner failed to start duty")
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

func (c *Committee) PushToQueue(slot phase0.Slot, dec *queue.SSVMessage) {
	c.mtx.RLock()
	queue, exists := c.Queues[slot]
	c.mtx.RUnlock()
	if !exists {
		c.logger.Warn("cannot push to non-existing queue", zap.Uint64("slot", uint64(slot)))
		return
	}

	if pushed := queue.Q.TryPush(dec); !pushed {
		c.logger.Warn("dropping ExecuteDuty message because the queue is full")
	}
}

// ProcessMessage processes Network Message of all types
func (c *Committee) ProcessMessage(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage) error {
	ctx, span := tracer.Start(ctx, fmt.Sprintf("%s.process_message", observabilityNamespace),
		trace.WithAttributes(
			attribute.String("ssv.validator.msg_id", msg.GetID().String()),
			attribute.Int64("ssv.validator.msg_type", int64(msg.GetType())),
			attribute.String("ssv.runner.role", msg.GetID().GetRoleType().String()),
		))
	defer span.End()

	// Validate message
	if msg.GetType() != message.SSVEventMsgType {
		if err := msg.SignedSSVMessage.Validate(); err != nil {
			err := errors.Wrap(err, "invalid SignedSSVMessage")
			span.SetStatus(codes.Error, err.Error())
			return err
		}

		// Verify SignedSSVMessage's signature
		if err := spectypes.Verify(msg.SignedSSVMessage, c.CommitteeMember.Committee); err != nil {
			err := errors.Wrap(err, "SignedSSVMessage has an invalid signature")
			span.SetStatus(codes.Error, err.Error())
			return err
		}

		if err := c.validateMessage(msg.SignedSSVMessage.SSVMessage); err != nil {
			err := errors.Wrap(err, "Message invalid")
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}

	switch msg.GetType() {
	case spectypes.SSVConsensusMsgType:
		qbftMsg := &qbft.Message{}
		if err := qbftMsg.Decode(msg.GetData()); err != nil {
			err := errors.Wrap(err, "could not get consensus Message from network Message")
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		if err := qbftMsg.Validate(); err != nil {
			err := errors.Wrap(err, "invalid qbft Message")
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		c.mtx.Lock()
		runner, exists := c.Runners[phase0.Slot(qbftMsg.Height)]
		c.mtx.Unlock()
		if !exists {
			err := errors.New("no runner found for message's slot")
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		if err := runner.ProcessConsensus(ctx, logger, msg.SignedSSVMessage); err != nil {
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		span.SetStatus(codes.Ok, "")
		return nil
	case spectypes.SSVPartialSignatureMsgType:
		pSigMessages := &spectypes.PartialSignatureMessages{}
		if err := pSigMessages.Decode(msg.SignedSSVMessage.SSVMessage.GetData()); err != nil {
			err := errors.Wrap(err, "could not get post consensus Message from network Message")
			span.SetStatus(codes.Error, err.Error())
			return err
		}

		// Validate
		if len(msg.SignedSSVMessage.OperatorIDs) != 1 {
			err := errors.New("PartialSignatureMessage has more than 1 signer")
			span.SetStatus(codes.Error, err.Error())
			return err
		}

		if err := pSigMessages.ValidateForSigner(msg.SignedSSVMessage.OperatorIDs[0]); err != nil {
			err := errors.Wrap(err, "invalid PartialSignatureMessages")
			span.SetStatus(codes.Error, err.Error())
			return err
		}

		if pSigMessages.Type == spectypes.PostConsensusPartialSig {
			c.mtx.Lock()
			runner, exists := c.Runners[pSigMessages.Slot]
			c.mtx.Unlock()
			if !exists {
				err := errors.New("no runner found for message's slot")
				span.SetStatus(codes.Error, err.Error())
				return err
			}

			if err := runner.ProcessPostConsensus(ctx, logger, pSigMessages); err != nil {
				span.SetStatus(codes.Error, err.Error())
				return err
			}
			span.SetStatus(codes.Ok, "")
			return nil
		}
	case message.SSVEventMsgType:
		if err := c.handleEventMessage(ctx, logger, msg); err != nil {
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		span.SetStatus(codes.Ok, "")
		return nil
	default:
		err := errors.New("unknown msg")
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

func (c *Committee) unsafePruneExpiredRunners(logger *zap.Logger, currentSlot phase0.Slot) error {
	if runnerExpirySlots > currentSlot {
		return nil
	}

	minValidSlot := currentSlot - runnerExpirySlots

	for slot := range c.Runners {
		if slot <= minValidSlot {
			opIds := types.OperatorIDsFromOperators(c.CommitteeMember.Committee)
			epoch := c.BeaconNetwork.EstimatedEpochAtSlot(slot)
			committeeDutyID := fields.FormatCommitteeDutyID(opIds, epoch, slot)
			logger = logger.With(fields.DutyID(committeeDutyID))
			logger.Debug("pruning expired committee runner", zap.Uint64("slot", uint64(slot)))
			delete(c.Runners, slot)
			delete(c.Queues, slot)
		}
	}

	return nil
}

func (c *Committee) Stop() {
	c.cancel()
}

func (c *Committee) Encode() ([]byte, error) {
	return json.Marshal(c)
}

func (c *Committee) Decode(data []byte) error {
	return json.Unmarshal(data, &c)
}

// GetRoot returns the state's deterministic root
func (c *Committee) GetRoot() ([32]byte, error) {
	marshaledRoot, err := c.Encode()
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "could not encode state")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}

func (c *Committee) MarshalJSON() ([]byte, error) {
	type CommitteeAlias struct {
		Runners         map[phase0.Slot]*runner.CommitteeRunner
		CommitteeMember *spectypes.CommitteeMember
		Share           map[phase0.ValidatorIndex]*spectypes.Share
	}

	// Create object and marshal
	alias := &CommitteeAlias{
		Runners:         c.Runners,
		CommitteeMember: c.CommitteeMember,
		Share:           c.Shares,
	}

	byts, err := json.Marshal(alias)

	return byts, err
}

func (c *Committee) UnmarshalJSON(data []byte) error {
	type CommitteeAlias struct {
		Runners         map[phase0.Slot]*runner.CommitteeRunner
		CommitteeMember *spectypes.CommitteeMember
		Shares          map[phase0.ValidatorIndex]*spectypes.Share
	}

	// Unmarshal the JSON data into the auxiliary struct
	aux := &CommitteeAlias{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Assign fields
	c.Runners = aux.Runners
	c.CommitteeMember = aux.CommitteeMember
	c.Shares = aux.Shares

	return nil
}

func (c *Committee) validateMessage(msg *spectypes.SSVMessage) error {
	if !(c.CommitteeMember.CommitteeID.MessageIDBelongs(msg.GetID())) {
		return errors.New("msg ID doesn't match committee ID")
	}

	if len(msg.GetData()) == 0 {
		return errors.New("msg data is invalid")
	}

	return nil
}
