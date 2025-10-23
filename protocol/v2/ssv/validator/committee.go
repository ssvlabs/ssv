package validator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
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
	logger = logger.With(zap.Uint64("current_slot", uint64(duty.Slot)))
	if err := c.unsafePruneExpiredRunners(logger, duty.Slot); err != nil {
		span.RecordError(err)
		logger.Error("couldn't prune expired committee runners", zap.Error(err))
	}

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
				Height:             qbft.Height(slot),
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
