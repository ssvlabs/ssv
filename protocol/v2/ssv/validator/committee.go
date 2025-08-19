package validator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	"github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/observability/traces"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

var (
	// runnerExpirySlots - Committee messages are allowed up to 34 slots in the future. All runners that are older can be stopped.
	runnerExpirySlots = phase0.Slot(34)
)

type CommitteeRunnerFunc func(slot phase0.Slot, shares map[phase0.ValidatorIndex]*spectypes.Share, attestingValidators []phase0.BLSPubKey, dutyGuard runner.CommitteeDutyGuard) (*runner.CommitteeRunner, error)

type Committee struct {
	logger *zap.Logger

	ctx    context.Context
	cancel context.CancelFunc

	networkConfig *networkconfig.Network

	// mtx syncs access to Queues, Runners, Shares.
	mtx     sync.RWMutex
	Queues  map[phase0.Slot]QueueContainer
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
	networkConfig *networkconfig.Network,
	operator *spectypes.CommitteeMember,
	createRunnerFn CommitteeRunnerFunc,
	shares map[phase0.ValidatorIndex]*spectypes.Share,
	dutyGuard *CommitteeDutyGuard,
) *Committee {
	if shares == nil {
		shares = make(map[phase0.ValidatorIndex]*spectypes.Share)
	}

	logger = logger.Named(log.NameCommittee).
		With(fields.Committee(types.OperatorIDsFromOperators(operator.Committee))).
		With(fields.CommitteeID(operator.CommitteeID))

	return &Committee{
		logger:          logger,
		networkConfig:   networkConfig,
		ctx:             ctx,
		cancel:          cancel,
		Queues:          make(map[phase0.Slot]QueueContainer),
		Runners:         make(map[phase0.Slot]*runner.CommitteeRunner),
		Shares:          shares,
		CommitteeMember: operator,
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

// StartDuty starts a new duty for the given slot.
func (c *Committee) StartDuty(ctx context.Context, logger *zap.Logger, duty *spectypes.CommitteeDuty) error {
	ctx, span := tracer.Start(ctx, observability.InstrumentName(observabilityNamespace, "start_committee_duty"))
	defer span.End()

	span.AddEvent("prepare duty and runner")
	r, runnableDuty, err := c.prepareDutyAndRunner(ctx, logger, duty)
	if err != nil {
		return traces.Error(span, err)
	}

	logger.Info("ℹ️ starting duty processing")
	err = r.StartNewDuty(ctx, logger, runnableDuty, c.CommitteeMember.GetQuorum())
	if err != nil {
		return traces.Errorf(span, "runner failed to start duty: %w", err)
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

func (c *Committee) prepareDutyAndRunner(ctx context.Context, logger *zap.Logger, duty *spectypes.CommitteeDuty) (
	r *runner.CommitteeRunner,
	runnableDuty *spectypes.CommitteeDuty,
	err error,
) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	_, span := tracer.Start(ctx, observability.InstrumentName(observabilityNamespace, "prepare_duty_runner"))
	defer span.End()

	if _, exists := c.Runners[duty.Slot]; exists {
		return nil, nil, traces.Errorf(span, "CommitteeRunner for slot %d already exists", duty.Slot)
	}

	shares, attesters, runnableDuty, err := c.prepareDuty(logger, duty)
	if err != nil {
		return nil, nil, traces.Error(span, err)
	}

	r, err = c.CreateRunnerFn(duty.Slot, shares, attesters, c.dutyGuard)
	if err != nil {
		return nil, nil, traces.Errorf(span, "could not create CommitteeRunner: %w", err)
	}

	// Set timeout function.
	r.GetBaseRunner().TimeoutF = c.onTimeout
	c.Runners[duty.Slot] = r
	_, queueExists := c.Queues[duty.Slot]
	if !queueExists {
		c.Queues[duty.Slot] = QueueContainer{
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
	logger = logger.With(zap.Uint64("current_slot", uint64(duty.Slot)))
	if err := c.unsafePruneExpiredRunners(logger, duty.Slot); err != nil {
		span.RecordError(err)
		logger.Error("couldn't prune expired committee runners", zap.Error(err))
	}

	span.SetStatus(codes.Ok, "")
	return r, runnableDuty, nil
}

// prepareDuty filters out unrunnable validator duties and returns the shares and attesters.
func (c *Committee) prepareDuty(logger *zap.Logger, duty *spectypes.CommitteeDuty) (
	shares map[phase0.ValidatorIndex]*spectypes.Share,
	attesters []phase0.BLSPubKey,
	runnableDuty *spectypes.CommitteeDuty,
	err error,
) {
	if len(duty.ValidatorDuties) == 0 {
		return nil, nil, nil, errors.New("no beacon duties")
	}

	runnableDuty = &spectypes.CommitteeDuty{
		Slot:            duty.Slot,
		ValidatorDuties: make([]*spectypes.ValidatorDuty, 0, len(duty.ValidatorDuties)),
	}
	shares = make(map[phase0.ValidatorIndex]*spectypes.Share, len(duty.ValidatorDuties))
	attesters = make([]phase0.BLSPubKey, 0, len(duty.ValidatorDuties))
	for _, beaconDuty := range duty.ValidatorDuties {
		share, exists := c.Shares[beaconDuty.ValidatorIndex]
		if !exists {
			// Filter out Beacon duties for which we don't have a share.
			logger.Debug("committee has no share for validator duty",
				fields.BeaconRole(beaconDuty.Type),
				zap.Uint64("validator_index", uint64(beaconDuty.ValidatorIndex)))
			continue
		}
		shares[beaconDuty.ValidatorIndex] = share
		runnableDuty.ValidatorDuties = append(runnableDuty.ValidatorDuties, beaconDuty)

		if beaconDuty.Type == spectypes.BNRoleAttester {
			attesters = append(attesters, phase0.BLSPubKey(share.SharePubKey))
		}
	}

	if len(shares) == 0 {
		return nil, nil, nil, errors.New("no shares for duty's validators")
	}

	return shares, attesters, runnableDuty, nil
}

// ProcessMessage processes Network Message of all types
func (c *Committee) ProcessMessage(ctx context.Context, msg *queue.SSVMessage) error {
	msgType := msg.GetType()
	msgID := msg.GetID()

	// Validate message (+ verify SignedSSVMessage's signature)
	if msgType != message.SSVEventMsgType {
		if err := msg.SignedSSVMessage.Validate(); err != nil {
			return fmt.Errorf("invalid SignedSSVMessage: %w", err)
		}
		if err := spectypes.Verify(msg.SignedSSVMessage, c.CommitteeMember.Committee); err != nil {
			return fmt.Errorf("SignedSSVMessage has an invalid signature: %w", err)
		}
		if err := c.validateMessage(msg.SignedSSVMessage.SSVMessage); err != nil {
			return fmt.Errorf("Message invalid: %w", err)
		}
	}

	slot, err := msg.Slot()
	if err != nil {
		return fmt.Errorf("couldn't get message slot: %w", err)
	}
	dutyID := fields.BuildCommitteeDutyID(types.OperatorIDsFromOperators(c.CommitteeMember.Committee), c.networkConfig.EstimatedEpochAtSlot(slot), slot)

	logger := c.logger.
		With(fields.MessageType(msgType)).
		With(fields.MessageID(msgID)).
		With(fields.RunnerRole(msgID.GetRoleType())).
		With(fields.Slot(slot)).
		With(fields.DutyID(dutyID))

	ctx, span := tracer.Start(traces.Context(ctx, dutyID),
		observability.InstrumentName(observabilityNamespace, "process_committee_message"),
		trace.WithAttributes(
			observability.ValidatorMsgTypeAttribute(msgType),
			observability.ValidatorMsgIDAttribute(msgID),
			observability.RunnerRoleAttribute(msgID.GetRoleType()),
			observability.CommitteeIDAttribute(c.CommitteeMember.CommitteeID),
			observability.BeaconSlotAttribute(slot),
			observability.DutyIDAttribute(dutyID),
		),
		trace.WithLinks(trace.LinkFromContext(msg.TraceContext)),
	)
	defer span.End()

	switch msgType {
	case spectypes.SSVConsensusMsgType:
		qbftMsg := &qbft.Message{}
		if err := qbftMsg.Decode(msg.GetData()); err != nil {
			return traces.Errorf(span, "could not decode consensus Message: %w", err)
		}
		if err := qbftMsg.Validate(); err != nil {
			return traces.Errorf(span, "invalid QBFT Message: %w", err)
		}
		c.mtx.RLock()
		r, exists := c.Runners[slot]
		c.mtx.RUnlock()
		if !exists {
			return traces.Errorf(span, "no runner found for message's slot")
		}
		return r.ProcessConsensus(ctx, logger, msg.SignedSSVMessage)
	case spectypes.SSVPartialSignatureMsgType:
		pSigMessages := &spectypes.PartialSignatureMessages{}
		if err := pSigMessages.Decode(msg.SignedSSVMessage.SSVMessage.GetData()); err != nil {
			return traces.Errorf(span, "could not decode PartialSignatureMessages: %w", err)
		}

		// Validate
		if len(msg.SignedSSVMessage.OperatorIDs) != 1 {
			return traces.Errorf(span, "PartialSignatureMessage has more than 1 signer")
		}

		if err := pSigMessages.ValidateForSigner(msg.SignedSSVMessage.OperatorIDs[0]); err != nil {
			return traces.Errorf(span, "invalid PartialSignatureMessages: %w", err)
		}

		if pSigMessages.Type == spectypes.PostConsensusPartialSig {
			c.mtx.RLock()
			r, exists := c.Runners[pSigMessages.Slot]
			c.mtx.RUnlock()
			if !exists {
				return traces.Errorf(span, "no runner found for message's slot")
			}
			if err := r.ProcessPostConsensus(ctx, logger, pSigMessages); err != nil {
				return traces.Error(span, err)
			}
			span.SetStatus(codes.Ok, "")
			return nil
		}
	case message.SSVEventMsgType:
		if err := c.handleEventMessage(ctx, logger, msg); err != nil {
			return traces.Errorf(span, "could not handle event message: %w", err)
		}
		span.SetStatus(codes.Ok, "")
		return nil
	default:
		return traces.Errorf(span, "unknown message type: %d", msgType)
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
			epoch := c.networkConfig.EstimatedEpochAtSlot(slot)
			committeeDutyID := fields.BuildCommitteeDutyID(opIds, epoch, slot)
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
