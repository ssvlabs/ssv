package validator

import (
	"context"
	"fmt"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	"github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"go.uber.org/zap"
)

type PreconfCommitmentRunnerFunc func(slot phase0.Slot) (*runner.PreconfCommitmentRunner, error)

type PreconfCommitment struct {
	logger *zap.Logger
	ctx    context.Context
	cancel context.CancelFunc

	BeaconNetwork spectypes.BeaconNetwork

	// mtx syncs access to Queues, Runners, Shares.
	mtx sync.RWMutex
	// Runners maps pre-confirmation ID to corresponding queue
	Queues map[string]queueContainer
	// Runners maps pre-confirmation ID to corresponding runner
	Runners map[string]*runner.PreconfCommitmentRunner
	Shares  map[phase0.ValidatorIndex]*spectypes.Share

	CommitteeMember *spectypes.CommitteeMember

	CreateRunnerFn PreconfCommitmentRunnerFunc
}

// NewPreconfCommitment creates a new cluster
func NewPreconfCommitment(
	ctx context.Context,
	cancel context.CancelFunc,
	logger *zap.Logger,
	beaconNetwork spectypes.BeaconNetwork,
	committeeMember *spectypes.CommitteeMember,
	createRunnerFn PreconfCommitmentRunnerFunc,
	shares map[phase0.ValidatorIndex]*spectypes.Share,
) *PreconfCommitment {
	if shares == nil {
		shares = make(map[phase0.ValidatorIndex]*spectypes.Share)
	}
	return &PreconfCommitment{
		logger:          logger,
		BeaconNetwork:   beaconNetwork,
		ctx:             ctx,
		cancel:          cancel,
		Queues:          make(map[phase0.Slot]queueContainer),
		Runners:         make(map[string]*runner.PreconfCommitmentRunner),
		Shares:          shares,
		CommitteeMember: committeeMember,
		CreateRunnerFn:  createRunnerFn,
	}
}

func (pc *PreconfCommitment) AddShare(share *spectypes.Share) {
	pc.mtx.Lock()
	defer pc.mtx.Unlock()
	pc.Shares[share.ValidatorIndex] = share
}

func (pc *PreconfCommitment) RemoveShare(validatorIndex phase0.ValidatorIndex) {
	pc.mtx.Lock()
	defer pc.mtx.Unlock()
	delete(pc.Shares, validatorIndex)
}

// StartDuty starts a new duty for the given slot
func (pc *PreconfCommitment) StartDuty(ctx context.Context, logger *zap.Logger, duty *spectypes.PreconfCommitmentDuty) error {
	r, trimmedDuty, err := pc.prepareDutyRunner(logger, duty)
	if err != nil {
		return fmt.Errorf("could not prepare duty runner: %w", err)
	}

	logger.Info("ℹ️ starting duty processing")
	err = r.StartNewDuty(ctx, logger, trimmedDuty, pc.CommitteeMember.GetQuorum())
	if err != nil {
		return errors.Wrap(err, "runner failed to start duty")
	}
	return nil
}

func (pc *PreconfCommitment) prepareDutyRunner(logger *zap.Logger, duty *spectypes.PreconfCommitmentDuty) (
	r *runner.PreconfCommitmentRunner,
	err error,
) {
	pc.mtx.Lock()
	defer pc.mtx.Unlock()

	if _, exists := pc.Runners[duty.Slot]; exists {
		return nil, fmt.Errorf("PreconfCommitmentRunner for slot %d already exists", duty.Slot)
	}

	r, err = pc.CreateRunnerFn(duty.Slot)
	if err != nil {
		return nil, errors.Wrap(err, "could not create PreconfCommitmentRunner")
	}

	pc.Runners[duty.Slot] = r
	_, queueExists := pc.Queues[duty.Slot]
	if !queueExists {
		pc.Queues[duty.Slot] = queueContainer{
			Q: queue.New(1000), // TODO alan: get queue opts from options
			queueState: &queue.State{
				HasRunningInstance: false,
				Height:             qbft.Height(duty.Slot),
				Slot:               duty.Slot,
				Quorum:             pc.CommitteeMember.GetQuorum(),
			},
		}
	}

	// Prunes all expired preconfCommitment runners, when new runner is created
	pruneLogger := pc.logger.With(zap.Uint64("current_slot", uint64(duty.Slot)))
	if err := pc.unsafePruneExpiredRunners(pruneLogger, duty.Slot); err != nil {
		pruneLogger.Error("couldn't prune expired preconfCommitment runners", zap.Error(err))
	}

	return r, nil
}

// ProcessMessage processes Network Message of all types
func (pc *PreconfCommitment) ProcessMessage(ctx context.Context, logger *zap.Logger, msg *queue.SSVMessage) error {
	// Validate message
	if msg.GetType() != message.SSVEventMsgType {
		if err := msg.SignedSSVMessage.Validate(); err != nil {
			return errors.Wrap(err, "invalid SignedSSVMessage")
		}

		// Verify SignedSSVMessage's signature
		if err := spectypes.Verify(msg.SignedSSVMessage, pc.CommitteeMember.Committee); err != nil {
			return errors.Wrap(err, "SignedSSVMessage has an invalid signature")
		}

		if err := pc.validateMessage(msg.SignedSSVMessage.SSVMessage); err != nil {
			return errors.Wrap(err, "Message invalid")
		}
	}

	switch msg.GetType() {
	case spectypes.SSVConsensusMsgType:
		return fmt.Errorf("consensus message is not expected, msg type: %d", msg.GetType())
	case spectypes.SSVPartialSignatureMsgType:
		pSigMessages := &spectypes.PartialSignatureMessages{}
		if err := pSigMessages.Decode(msg.SignedSSVMessage.SSVMessage.GetData()); err != nil {
			return errors.Wrap(err, "could not get post consensus Message from network Message")
		}
		if len(msg.SignedSSVMessage.OperatorIDs) != 1 {
			return errors.New("PartialSignatureMessage has more than 1 signer")
		}
		if err := pSigMessages.ValidateForSigner(msg.SignedSSVMessage.OperatorIDs[0]); err != nil {
			return errors.Wrap(err, "invalid PartialSignatureMessages")
		}
		if pSigMessages.Type != spectypes.TODO {
			pc.mtx.RLock()
			r, exists := pc.Runners[pSigMessages.Slot]
			pc.mtx.RUnlock()
			if !exists {
				return fmt.Errorf("no runner found for post consensus partial-sig message's slot %d", pSigMessages.Slot)
			}
			return r.ProcessPreConsensus(ctx, logger, pSigMessages)
		}
		return fmt.Errorf("partial-signature message is not expected, msg type: %d", msg.GetType())
	case message.SSVEventMsgType:
		return pc.handleEventMessage(ctx, logger, msg)
	default:
		return fmt.Errorf("unknown message, msg type: %d", msg.GetType())
	}
}

func (pc *PreconfCommitment) unsafePruneExpiredRunners(logger *zap.Logger, currentSlot phase0.Slot) error {
	if runnerExpirySlots > currentSlot {
		return nil
	}

	minValidSlot := currentSlot - runnerExpirySlots

	for slot := range pc.Runners {
		if slot <= minValidSlot {
			opIds := types.OperatorIDsFromOperators(pc.CommitteeMember.PreconfCommitment)
			epoch := pc.BeaconNetwork.EstimatedEpochAtSlot(slot)
			preconfCommitmentDutyID := fields.FormatPreconfCommitmentDutyID(opIds, epoch, slot)
			logger = logger.With(fields.DutyID(preconfCommitmentDutyID))
			logger.Debug("pruning expired preconfCommitment runner", zap.Uint64("slot", uint64(slot)))
			delete(pc.Runners, slot)
			delete(pc.Queues, slot)
		}
	}

	return nil
}

func (pc *PreconfCommitment) Stop() {
	pc.cancel()
}

func (pc *PreconfCommitment) validateMessage(msg *spectypes.SSVMessage) error {
	if !(pc.CommitteeMember.CommitteeID.MessageIDBelongs(msg.GetID())) {
		return errors.New("msg ID doesn't match preconfCommitment ID")
	}

	if len(msg.GetData()) == 0 {
		return errors.New("msg data is invalid")
	}

	return nil
}
