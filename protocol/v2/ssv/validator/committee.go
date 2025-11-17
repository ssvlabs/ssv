package validator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
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

type CommitteeRunnerFunc func(slot phase0.Slot, shares map[phase0.ValidatorIndex]*spectypes.Share, attestingValidators []phase0.BLSPubKey, dutyGuard runner.CommitteeDutyGuard) (*runner.CommitteeRunner, error)

type Committee struct {
	logger *zap.Logger

	networkConfig *networkconfig.Network

	// mtx syncs access to Queues, Runners, Shares.
	mtx     sync.RWMutex
	Queues  map[phase0.Slot]queueContainer
	Runners map[phase0.Slot]*runner.CommitteeRunner
	Shares  map[phase0.ValidatorIndex]*spectypes.Share

	CommitteeMember *spectypes.CommitteeMember

	dutyGuard      *CommitteeDutyGuard
	CreateRunnerFn CommitteeRunnerFunc
}

// NewCommittee creates a new cluster
func NewCommittee(
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
		Queues:          make(map[phase0.Slot]queueContainer),
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
func (c *Committee) StartDuty(ctx context.Context, logger *zap.Logger, duty *spectypes.CommitteeDuty) (
	*runner.CommitteeRunner,
	queueContainer,
	error,
) {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "start_committee_duty"),
		trace.WithAttributes(
			observability.RunnerRoleAttribute(duty.RunnerRole()),
			observability.DutyCountAttribute(len(duty.ValidatorDuties)),
			observability.BeaconSlotAttribute(duty.Slot)))
	defer span.End()

	span.AddEvent("prepare duty and runner")
	r, q, runnableDuty, err := c.prepareDutyAndRunner(ctx, logger, duty)
	if err != nil {
		return nil, queueContainer{}, traces.Errorf(span, "prepare duty and runner: %w", err)
	}

	logger.Info("ℹ️ starting duty processing")
	err = r.StartNewDuty(ctx, logger, runnableDuty, c.CommitteeMember.GetQuorum())
	if err != nil {
		return nil, queueContainer{}, traces.Errorf(span, "runner failed to start duty: %w", err)
	}

	span.SetStatus(codes.Ok, "")
	return r, q, nil
}

func (c *Committee) prepareDutyAndRunner(ctx context.Context, logger *zap.Logger, duty *spectypes.CommitteeDuty) (
	r *runner.CommitteeRunner,
	q queueContainer,
	runnableDuty *spectypes.CommitteeDuty,
	err error,
) {
	_, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "prepare_duty_runner"),
		trace.WithAttributes(
			observability.RunnerRoleAttribute(duty.RunnerRole()),
			observability.DutyCountAttribute(len(duty.ValidatorDuties)),
			observability.BeaconSlotAttribute(duty.Slot)))
	defer span.End()

	c.mtx.Lock()
	defer c.mtx.Unlock()

	if _, exists := c.Runners[duty.Slot]; exists {
		return nil, queueContainer{}, nil, traces.Errorf(span, "CommitteeRunner for slot %d already exists", duty.Slot)
	}

	shares, attesters, runnableDuty, err := c.prepareDuty(logger, duty)
	if err != nil {
		return nil, queueContainer{}, nil, traces.Error(span, err)
	}

	// Create the corresponding runner.
	r, err = c.CreateRunnerFn(duty.Slot, shares, attesters, c.dutyGuard)
	if err != nil {
		return nil, queueContainer{}, nil, traces.Errorf(span, "could not create CommitteeRunner: %w", err)
	}
	r.SetTimeoutFunc(c.onTimeout)
	c.Runners[duty.Slot] = r

	// Initialize the corresponding queue preemptively (so we can skip this during duty execution).
	q = c.getQueue(logger, duty.Slot)

	// Prunes all expired committee runners opportunistically (when a new runner is created).
	c.unsafePruneExpiredRunners(logger, duty.Slot)

	span.SetStatus(codes.Ok, "")
	return r, q, runnableDuty, nil
}

// getQueue returns queue for the provided slot, lazily initializing it if it didn't exist previously.
// MUST be called with c.mtx locked!
func (c *Committee) getQueue(logger *zap.Logger, slot phase0.Slot) queueContainer {
	q, exists := c.Queues[slot]
	if !exists {
		q = queueContainer{
			Q: queue.New(
				logger,
				1000,
				queue.WithInboxSizeMetric(
					queue.InboxSizeMetric,
					queue.CommitteeQueueMetricType,
					queue.CommitteeMetricID(slot),
				),
			),
			queueState: &queue.State{
				HasRunningInstance: false,
				Height:             specqbft.Height(slot),
				Slot:               slot,
				Quorum:             c.CommitteeMember.GetQuorum(),
			},
		}
		c.Queues[slot] = q
	}

	return q
}

// prepareDuty filters out unrunnable validator duties and returns the shares and attesters.
func (c *Committee) prepareDuty(logger *zap.Logger, duty *spectypes.CommitteeDuty) (
	shares map[phase0.ValidatorIndex]*spectypes.Share,
	attesters []phase0.BLSPubKey,
	runnableDuty *spectypes.CommitteeDuty,
	err error,
) {
	if len(duty.ValidatorDuties) == 0 {
		return nil, nil, nil, spectypes.NewError(spectypes.NoBeaconDutiesErrorCode, "no beacon duties")
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
		return nil, nil, nil, spectypes.NewError(spectypes.NoValidatorSharesErrorCode, "no shares for duty's validators")
	}

	return shares, attesters, runnableDuty, nil
}

// ProcessMessage processes p2p message of all types
func (c *Committee) ProcessMessage(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage) error {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	span.AddEvent("got committee message to process")

	msgType := msg.GetType()

	// Validate message (+ verify SignedSSVMessage's signature)
	if msgType != message.SSVEventMsgType {
		if err := msg.SignedSSVMessage.Validate(); err != nil {
			return fmt.Errorf("validate SignedSSVMessage: %w", err)
		}
		if err := spectypes.Verify(msg.SignedSSVMessage, c.CommitteeMember.Committee); err != nil {
			return spectypes.WrapError(spectypes.SSVMessageHasInvalidSignatureErrorCode, fmt.Errorf("verify SignedSSVMessage signatures: %w", err))
		}
		if err := c.validateMessage(msg.SignedSSVMessage.SSVMessage); err != nil {
			return fmt.Errorf("validate SignedSSVMessage.SSVMessage: %w", err)
		}
	}

	slot, err := msg.Slot()
	if err != nil {
		return fmt.Errorf("couldn't get message slot: %w", err)
	}

	switch msgType {
	case spectypes.SSVConsensusMsgType:
		span.AddEvent("process committee message = consensus message")

		qbftMsg := &specqbft.Message{}
		if err := qbftMsg.Decode(msg.GetData()); err != nil {
			return fmt.Errorf("could not decode consensus Message: %w", err)
		}
		if err := qbftMsg.Validate(); err != nil {
			return fmt.Errorf("validate QBFT message: %w", err)
		}

		c.mtx.RLock()
		r, exists := c.Runners[slot]
		c.mtx.RUnlock()
		if !exists {
			return spectypes.WrapError(spectypes.NoRunnerForSlotErrorCode, fmt.Errorf("no runner found for message's slot %d", slot))
		}

		return r.ProcessConsensus(ctx, logger, msg.SignedSSVMessage)
	case spectypes.SSVPartialSignatureMsgType:
		pSigMessages := &spectypes.PartialSignatureMessages{}
		if err := pSigMessages.Decode(msg.SignedSSVMessage.SSVMessage.GetData()); err != nil {
			return fmt.Errorf("could not decode PartialSignatureMessages: %w", err)
		}

		// Validate
		if len(msg.SignedSSVMessage.OperatorIDs) != 1 {
			return fmt.Errorf("PartialSignatureMessage has %d signers (must be 1 signer)", len(msg.SignedSSVMessage.OperatorIDs))
		}

		if err := pSigMessages.ValidateForSigner(msg.SignedSSVMessage.OperatorIDs[0]); err != nil {
			return fmt.Errorf("PartialSignatureMessages signer is invalid: %w", err)
		}

		if pSigMessages.Type == spectypes.PostConsensusPartialSig {
			span.AddEvent("process committee message = post-consensus message")

			c.mtx.RLock()
			r, exists := c.Runners[pSigMessages.Slot]
			c.mtx.RUnlock()
			if !exists {
				return spectypes.WrapError(spectypes.NoRunnerForSlotErrorCode, fmt.Errorf("no runner found for message's slot"))
			}
			if err := r.ProcessPostConsensus(ctx, logger, pSigMessages); err != nil {
				return fmt.Errorf("process post-consensus message: %w", err)
			}
		}

		return nil
	case message.SSVEventMsgType:
		eventMsg, ok := msg.Body.(*types.EventMsg)
		if !ok {
			return fmt.Errorf("could not decode event message (slot=%d)", slot)
		}

		span.SetAttributes(observability.ValidatorEventTypeAttribute(eventMsg.Type))

		switch eventMsg.Type {
		case types.Timeout:
			span.AddEvent("process committee message = event(timeout)")

			c.mtx.RLock()
			dutyRunner, found := c.Runners[slot]
			c.mtx.RUnlock()
			if !found {
				return fmt.Errorf("no committee runner found for slot %d", slot)
			}

			timeoutData, err := eventMsg.GetTimeoutData()
			if err != nil {
				return fmt.Errorf("get timeout data: %w", err)
			}

			if err := dutyRunner.OnTimeoutQBFT(ctx, logger, timeoutData); err != nil {
				return fmt.Errorf("timeout event: %w", err)
			}

			return nil
		default:
			return fmt.Errorf("unknown event msg - %s", eventMsg.Type.String())
		}
	default:
		return fmt.Errorf("unknown message type: %d", msgType)
	}
}

func (c *Committee) unsafePruneExpiredRunners(logger *zap.Logger, currentSlot phase0.Slot) {
	const lateSlotAllowance = 2 // LateSlotAllowance from message/validation/const.go
	runnerExpirySlots := phase0.Slot(c.networkConfig.SlotsPerEpoch + lateSlotAllowance)

	if currentSlot <= runnerExpirySlots {
		return // nothing to prune yet
	}

	minValidSlot := currentSlot - runnerExpirySlots

	for slot := range c.Runners {
		if slot < minValidSlot {
			opIds := types.OperatorIDsFromOperators(c.CommitteeMember.Committee)
			epoch := c.networkConfig.EstimatedEpochAtSlot(slot)
			committeeDutyID := fields.BuildCommitteeDutyID(opIds, epoch, slot)
			logger = logger.With(fields.DutyID(committeeDutyID))
			logger.Debug("pruning expired committee runner", zap.Uint64("prune_slot", uint64(slot)))
			delete(c.Runners, slot)
			delete(c.Queues, slot)
		}
	}
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
		return spectypes.NewError(spectypes.MessageIDCommitteeIDMismatchErrorCode, "msg ID doesn't match committee ID")
	}

	if len(msg.GetData()) == 0 {
		return errors.New("msg data is invalid")
	}

	return nil
}
