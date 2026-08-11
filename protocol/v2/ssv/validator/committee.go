package validator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
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

type CommitteeRunnerFunc func(
	duty spectypes.Duty,
	shares map[phase0.ValidatorIndex]*spectypes.Share,
	attestingValidators []phase0.BLSPubKey,
	dutyGuard runner.CommitteeDutyGuard,
) (runner.Runner, error)

type Committee struct {
	logger *zap.Logger

	networkConfig *networkconfig.Network

	// mtx syncs access to Queues, AggregatorQueues, Runners, AggregatorRunners, Shares.
	mtx sync.RWMutex
	// Queues is used for Committee duties (attestations and sync committees).
	Queues map[phase0.Slot]queueContainer
	// AggregatorQueues is used for AggregatorCommittee duties (aggregations and sync committee contributions).
	AggregatorQueues  map[phase0.Slot]queueContainer
	Runners           map[phase0.Slot]*runner.CommitteeRunner
	AggregatorRunners map[phase0.Slot]*runner.AggregatorCommitteeRunner
	Shares            map[phase0.ValidatorIndex]*spectypes.Share

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
		logger:            logger,
		networkConfig:     networkConfig,
		Queues:            make(map[phase0.Slot]queueContainer),
		AggregatorQueues:  make(map[phase0.Slot]queueContainer),
		Runners:           make(map[phase0.Slot]*runner.CommitteeRunner),
		AggregatorRunners: make(map[phase0.Slot]*runner.AggregatorCommitteeRunner),
		Shares:            shares,
		CommitteeMember:   operator,
		CreateRunnerFn:    createRunnerFn,
		dutyGuard:         dutyGuard,
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
func (c *Committee) StartDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) (
	runner.Runner,
	queueContainer,
	error,
) {
	role := types.RunnerRoleForDuty(duty, c.networkConfig.BooleForkAtSlot(duty.DutySlot()))
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "start_committee_duty"),
		trace.WithAttributes(
			observability.RunnerRoleAttribute(role),
			observability.DutyCountAttribute(len(c.extractValidatorDuties(duty))),
			observability.BeaconSlotAttribute(duty.DutySlot())))
	defer span.End()

	span.AddEvent("prepare duty and runner")
	commRunner, q, runnableDuty, err := c.prepareDutyAndRunner(ctx, logger, duty)
	if err != nil {
		return nil, queueContainer{}, traces.Errorf(span, "prepare duty and runner: %w", err)
	}

	logger.Info("ℹ️ starting duty processing")
	err = commRunner.StartNewDuty(ctx, logger, runnableDuty, c.CommitteeMember.GetQuorum())
	if err != nil {
		return nil, queueContainer{}, traces.Errorf(span, "runner failed to start duty: %w", err)
	}

	span.SetStatus(codes.Ok, "")
	return commRunner, q, nil
}

func (c *Committee) prepareDutyAndRunner(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) (
	commRunner runner.Runner,
	q queueContainer,
	runnableDuty spectypes.Duty,
	err error,
) {
	validatorDuties := c.extractValidatorDuties(duty)
	role := types.RunnerRoleForDuty(duty, c.networkConfig.BooleForkAtSlot(duty.DutySlot()))

	_, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "prepare_duty_runner"),
		trace.WithAttributes(
			observability.RunnerRoleAttribute(role),
			observability.DutyCountAttribute(len(validatorDuties)),
			observability.BeaconSlotAttribute(duty.DutySlot())))
	defer span.End()

	c.mtx.Lock()
	defer c.mtx.Unlock()

	if _, ok := c.runnerForDuty(duty); ok {
		return nil, queueContainer{}, nil, traces.Errorf(span, "committee runner for slot %d already exists", duty.DutySlot())
	}

	shares, attesters, runnableDuty, err := c.prepareDuty(logger, duty)
	if err != nil {
		return nil, queueContainer{}, nil, traces.Error(span, err)
	}

	commRunner, err = c.createRunner(duty, shares, attesters)
	if err != nil {
		return nil, queueContainer{}, nil, traces.Error(span, err)
	}

	// Initialize the corresponding queue preemptively (so we can skip this during duty execution).
	q = c.getQueueForRole(logger, duty.DutySlot(), role)

	// Prunes all expired committee runners opportunistically (when a new runner is created).
	c.unsafePruneExpiredRunners(logger, duty.DutySlot())

	span.SetStatus(codes.Ok, "")
	return commRunner, q, runnableDuty, nil
}

// getQueue returns queue for the provided slot, lazily initializing it if it didn't exist previously.
// MUST be called with c.mtx locked!
func (c *Committee) getQueueForRole(logger *zap.Logger, slot phase0.Slot, role spectypes.RunnerRole) queueContainer {
	// Select backing map by role.
	var m map[phase0.Slot]queueContainer
	var assign func(slot phase0.Slot, qc queueContainer)

	switch role {
	case spectypes.RoleAggregatorCommittee:
		m = c.AggregatorQueues
		assign = func(slot phase0.Slot, qc queueContainer) { c.AggregatorQueues[slot] = qc }
	case spectypes.RoleCommittee:
		m = c.Queues
		assign = func(slot phase0.Slot, qc queueContainer) { c.Queues[slot] = qc }
	default:
		c.logger.Panic("BUG: unexpected committee queue role", fields.RunnerRole(role))
	}

	q, exists := m[slot]
	if !exists {
		qType := queue.CommitteeQueueMetricType
		if role == spectypes.RoleAggregatorCommittee {
			qType = queue.AggregatorCommitteeQueueMetricType
		}
		q = queueContainer{
			Q: queue.New(
				logger,
				defaultValidatorQueueSize,
				queue.WithQueueMetrics(
					queue.InboxSizeMetric,
					qType,
					queue.CommitteeMetricID(slot),
				),
			),
		}
		assign(slot, q)
	}
	return q
}

// prepareDuty filters out unrunnable validator duties and returns the shares and attesters.
func (c *Committee) prepareDuty(logger *zap.Logger, duty spectypes.Duty) (
	shares map[phase0.ValidatorIndex]*spectypes.Share,
	attesters []phase0.BLSPubKey,
	runnableDuty spectypes.Duty,
	err error,
) {
	validatorDuties := c.extractValidatorDuties(duty)
	if len(validatorDuties) == 0 {
		return nil, nil, nil,
			spectypes.NewError(spectypes.NoBeaconDutiesErrorCode, "no beacon duties")
	}

	runnableValidatorDuties := make([]*spectypes.ValidatorDuty, 0, len(validatorDuties))

	shares = make(map[phase0.ValidatorIndex]*spectypes.Share, len(validatorDuties))
	attesters = make([]phase0.BLSPubKey, 0, len(validatorDuties))
	for _, beaconDuty := range validatorDuties {
		share, exists := c.Shares[beaconDuty.ValidatorIndex]
		if !exists {
			// Filter out Beacon duties for which we don't have a share.
			logger.Debug("committee has no share for validator duty",
				fields.BeaconRole(beaconDuty.Type),
				zap.Uint64("validator_index", uint64(beaconDuty.ValidatorIndex)))
			continue
		}
		shares[beaconDuty.ValidatorIndex] = share
		runnableValidatorDuties = append(runnableValidatorDuties, beaconDuty)

		if beaconDuty.Type == spectypes.BNRoleAttester {
			attesters = append(attesters, phase0.BLSPubKey(share.SharePubKey))
		}
	}

	if len(shares) == 0 {
		return nil, nil, nil,
			spectypes.NewError(spectypes.NoValidatorSharesErrorCode, "no shares for duty's validators")
	}

	switch duty := duty.(type) {
	case *spectypes.CommitteeDuty:
		runnableDuty = &spectypes.CommitteeDuty{
			Slot:            duty.Slot,
			ValidatorDuties: runnableValidatorDuties,
		}
	case *spectypes.AggregatorCommitteeDuty:
		runnableDuty = &spectypes.AggregatorCommitteeDuty{
			Slot:            duty.Slot,
			ValidatorDuties: runnableValidatorDuties,
		}
	default:
		c.logger.Panic("BUG: unexpected duty type", zap.String("type", fmt.Sprintf("%T", duty)))
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

	role := msg.GetID().GetRoleType()
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
		r, ok := c.runnerForRole(role, slot)
		c.mtx.RUnlock()
		if !ok {
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

		// Locate the runner for this slot once and route by message subtype.
		c.mtx.RLock()
		r, ok := c.runnerForRole(role, slot)
		c.mtx.RUnlock()
		if !ok {
			return spectypes.WrapError(spectypes.NoRunnerForSlotErrorCode, fmt.Errorf("no runner found for message's slot %d", slot))
		}

		if pSigMessages.Type == spectypes.PostConsensusPartialSig {
			span.AddEvent("process committee message = post-consensus message")
			if err := r.ProcessPostConsensus(ctx, logger, pSigMessages); err != nil {
				return fmt.Errorf("process post-consensus message: %w", err)
			}
			return nil
		}

		if role != spectypes.RoleAggregatorCommittee {
			return fmt.Errorf("invalid aggregator partial sig msg for committee role")
		}

		// Handle all non-post consensus partial signatures via pre-consensus path
		// (e.g., aggregator selection proofs and sync committee selection proofs).
		span.AddEvent("process committee message = pre-consensus message")
		if err := r.ProcessPreConsensus(ctx, logger, pSigMessages); err != nil {
			return fmt.Errorf("process pre-consensus message: %w", err)
		}
		return nil
	case message.SSVEventMsgType:
		eventMsg, ok := msg.Body.(*types.EventMsg)
		if !ok || eventMsg == nil {
			return fmt.Errorf("could not decode event message (slot=%d)", slot)
		}

		span.SetAttributes(observability.ValidatorEventTypeAttribute(eventMsg.Type))

		switch eventMsg.Type {
		case types.Timeout:
			span.AddEvent("process committee message = event(timeout)")

			c.mtx.RLock()
			r, found := c.runnerForRole(role, slot)
			c.mtx.RUnlock()
			if !found {
				// Old runners are pruned, timeout-event issuer is unaware of that - that's why we can end up here
				logger.Debug("event message: timeout event arrived, but targeted runner not found (likely was pruned)")
				return nil
			}

			timeoutData, err := eventMsg.GetTimeoutData()
			if err != nil {
				return fmt.Errorf("event message: get timeout data: %w", err)
			}

			if err := r.OnQBFTRoundTimeout(ctx, logger, timeoutData); err != nil {
				return fmt.Errorf("event message: process timeout event: %w", err)
			}

			return nil
		default:
			return fmt.Errorf("event message: unknown msg type - %s", eventMsg.Type.String())
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
			committeeDutyID := fields.BuildCommitteeDutyID(opIds, epoch, slot, spectypes.RoleCommittee)
			logger = logger.With(fields.DutyID(committeeDutyID))
			logger.Debug("pruning expired committee runner", zap.Uint64("prune_slot", uint64(slot)))
			delete(c.Runners, slot)
			delete(c.Queues, slot)
		}
	}

	for slot := range c.AggregatorRunners {
		if slot < minValidSlot {
			opIds := types.OperatorIDsFromOperators(c.CommitteeMember.Committee)
			epoch := c.networkConfig.EstimatedEpochAtSlot(slot)
			committeeDutyID := fields.BuildCommitteeDutyID(opIds, epoch, slot, spectypes.RoleAggregatorCommittee)
			logger = logger.With(fields.DutyID(committeeDutyID))
			logger.Debug("pruning expired aggregator committee runner", zap.Uint64("slot", uint64(slot)))
			delete(c.AggregatorRunners, slot)
			delete(c.AggregatorQueues, slot)
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
		return [32]byte{}, fmt.Errorf("could not encode state: %w", err)
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}

func (c *Committee) MarshalJSON() ([]byte, error) {
	type CommitteeAlias struct {
		Runners           map[phase0.Slot]*runner.CommitteeRunner
		AggregatorRunners map[phase0.Slot]*runner.AggregatorCommitteeRunner
		CommitteeMember   *spectypes.CommitteeMember
		Share             map[phase0.ValidatorIndex]*spectypes.Share
	}

	// Create object and marshal
	alias := &CommitteeAlias{
		Runners:           c.Runners,
		AggregatorRunners: c.AggregatorRunners,
		CommitteeMember:   c.CommitteeMember,
		Share:             c.Shares,
	}

	byts, err := json.Marshal(alias)

	return byts, err
}

func (c *Committee) UnmarshalJSON(data []byte) error {
	type CommitteeAlias struct {
		Runners           map[phase0.Slot]*runner.CommitteeRunner
		AggregatorRunners map[phase0.Slot]*runner.AggregatorCommitteeRunner
		CommitteeMember   *spectypes.CommitteeMember
		Shares            map[phase0.ValidatorIndex]*spectypes.Share
	}

	// Unmarshal the JSON data into the auxiliary struct
	aux := &CommitteeAlias{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Assign fields
	c.Runners = aux.Runners
	c.AggregatorRunners = aux.AggregatorRunners
	c.CommitteeMember = aux.CommitteeMember
	c.Shares = aux.Shares

	return nil
}

func (c *Committee) validateMessage(msg *spectypes.SSVMessage) error {
	if !(c.CommitteeMember.CommitteeID.MessageIDBelongs(msg.GetID())) {
		return spectypes.NewError(spectypes.MessageIDCommitteeIDMismatchErrorCode, "msg ID doesn't match committee ID")
	}

	role := msg.GetID().GetRoleType()
	if role != spectypes.RoleCommittee && role != spectypes.RoleAggregatorCommittee {
		return spectypes.NewError(spectypes.CommitteeWrongRoleErrorCode, "msg role is invalid")
	}

	if len(msg.GetData()) == 0 {
		return errors.New("msg data is invalid")
	}

	return nil
}

func (c *Committee) runnerForDuty(duty spectypes.Duty) (runner.Runner, bool) {
	switch duty.(type) {
	case *spectypes.CommitteeDuty:
		r, ok := c.Runners[duty.DutySlot()]
		return r, ok
	case *spectypes.AggregatorCommitteeDuty:
		r, ok := c.AggregatorRunners[duty.DutySlot()]
		return r, ok
	default:
		return nil, false
	}
}

func (c *Committee) runnerForRole(role spectypes.RunnerRole, slot phase0.Slot) (runner.Runner, bool) {
	switch role {
	case spectypes.RoleCommittee:
		r, ok := c.Runners[slot]
		return r, ok
	case spectypes.RoleAggregatorCommittee:
		r, ok := c.AggregatorRunners[slot]
		return r, ok
	default:
		return nil, false
	}
}

func (c *Committee) createRunner(
	duty spectypes.Duty,
	shares map[phase0.ValidatorIndex]*spectypes.Share,
	attesters []phase0.BLSPubKey,
) (runner.Runner, error) {
	r, err := c.CreateRunnerFn(duty, shares, attesters, c.dutyGuard)
	if err != nil {
		return nil, fmt.Errorf("create committee runner: %w", err)
	}

	// Wire the QBFT round-timer factory, bound to a msg ID carrying this duty's role so timeout
	// events are routed to the matching (committee vs aggregator-committee) slot queue.
	role := types.RunnerRoleForDuty(duty, c.networkConfig.BooleForkAtSlot(duty.DutySlot()))
	// Derive the domain from the duty's own slot (like every other MsgID in the fork cutover) rather
	// than the current wall-clock slot, so a pre-fork duty still running after the fork keys its
	// timer events under the right domain. Only GetRoleType() is read from this ID downstream, so
	// this is a consistency fix, not a behavior change today.
	runnerIdentifier := spectypes.NewMsgID(c.networkConfig.DomainTypeAtSlot(duty.DutySlot()), c.CommitteeMember.CommitteeID[:], role)
	r.SetQBFTRoundTimerF(c.newQBFTRoundTimerF(runnerIdentifier))

	switch duty := duty.(type) {
	case *spectypes.CommitteeDuty:
		cr, ok := r.(*runner.CommitteeRunner)
		if !ok {
			return nil, fmt.Errorf("BUG: runner created for committee duty has type %T, expected *runner.CommitteeRunner", r)
		}
		c.Runners[duty.DutySlot()] = cr
	case *spectypes.AggregatorCommitteeDuty:
		ar, ok := r.(*runner.AggregatorCommitteeRunner)
		if !ok {
			return nil, fmt.Errorf("BUG: runner created for aggregator committee duty has type %T, expected *runner.AggregatorCommitteeRunner", r)
		}
		c.AggregatorRunners[duty.DutySlot()] = ar
	default:
		c.logger.Panic("BUG: attempt to create committee runner with non-committee duty type",
			zap.String("type", fmt.Sprintf("%T", duty)))
	}

	return r, err
}

func (c *Committee) extractValidatorDuties(duty spectypes.Duty) []*spectypes.ValidatorDuty {
	switch duty := duty.(type) {
	case *spectypes.CommitteeDuty:
		return duty.ValidatorDuties
	case *spectypes.AggregatorCommitteeDuty:
		return duty.ValidatorDuties
	default:
		c.logger.Panic("BUG: attempt to extract validator duties from non-committee duty type",
			zap.String("type", fmt.Sprintf("%T", duty)))
		panic("BUG: unreachable")
	}
}
