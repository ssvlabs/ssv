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
	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"go.uber.org/zap"
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

	BeaconNetwork spectypes.BeaconNetwork
	Storage       *storage.QBFTStores

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
	ctx context.Context,
	cancel context.CancelFunc,
	logger *zap.Logger,
	beaconNetwork spectypes.BeaconNetwork,
	committeeMember *spectypes.CommitteeMember,
	createRunnerFn CommitteeRunnerFunc,
	shares map[phase0.ValidatorIndex]*spectypes.Share,
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
		dutyGuard:       NewCommitteeDutyGuard(),
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

// StartDuty starts a new duty for the given slot
func (c *Committee) StartDuty(logger *zap.Logger, duty *spectypes.CommitteeDuty) error {
	prepareRunner := func() (runner.Runner, error) {
		c.mtx.Lock()
		defer c.mtx.Unlock()

		if len(duty.ValidatorDuties) == 0 {
			return nil, errors.New("no beacon duties")
		}
		if _, exists := c.Runners[duty.Slot]; exists {
			return nil, errors.New(fmt.Sprintf("CommitteeRunner for slot %d already exists", duty.Slot))
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
				logger.Debug("no share for validator duty",
					fields.BeaconRole(beaconDuty.Type),
					zap.Uint64("validator_index", uint64(beaconDuty.ValidatorIndex)))
				continue
			}
			shares[beaconDuty.ValidatorIndex] = share
			filteredDuty.ValidatorDuties = append(filteredDuty.ValidatorDuties, beaconDuty)

			if beaconDuty.Type == spectypes.BNRoleAttester {
				attesters = append(attesters, share.SharePubKey)
			}
		}
		if len(shares) == 0 {
			return nil, errors.New("no shares for duty's validators")
		}
		duty = filteredDuty

		r, err := c.CreateRunnerFn(duty.Slot, shares, attesters, c.dutyGuard)
		if err != nil {
			return nil, errors.Wrap(err, "could not create CommitteeRunner")
		}

		// Set timeout function.
		r.GetBaseRunner().TimeoutF = c.onTimeout
		c.Runners[duty.Slot] = r
		_, queueExists := c.Queues[duty.Slot]
		if !queueExists {
			c.Queues[duty.Slot] = queueContainer{
				Q: queue.WithMetrics(queue.New(1000), nil), // TODO alan: get queue opts from options
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
			pruneLogger.Error("couldn't prune expired committee runners", zap.Error(err))
		}

		return r, nil
	}

	r, err := prepareRunner()
	if err != nil {
		return err
	}

	logger.Info("ℹ️ starting duty processing")
	err = r.StartNewDuty(logger, duty, c.CommitteeMember.GetQuorum())
	if err != nil {
		return errors.Wrap(err, "runner failed to start duty")
	}
	return nil
}

// ProcessMessage processes Network Message of all types
func (c *Committee) ProcessMessage(logger *zap.Logger, msg *queue.SSVMessage) error {
	// Validate message
	if msg.GetType() != message.SSVEventMsgType {
		if err := msg.SignedSSVMessage.Validate(); err != nil {
			return errors.Wrap(err, "invalid SignedSSVMessage")
		}

		// Verify SignedSSVMessage's signature
		if err := spectypes.Verify(msg.SignedSSVMessage, c.CommitteeMember.Committee); err != nil {
			return errors.Wrap(err, "SignedSSVMessage has an invalid signature")
		}

		if err := c.validateMessage(msg.SignedSSVMessage.SSVMessage); err != nil {
			return errors.Wrap(err, "Message invalid")
		}
	}

	switch msg.GetType() {
	case spectypes.SSVConsensusMsgType:
		qbftMsg := &qbft.Message{}
		if err := qbftMsg.Decode(msg.GetData()); err != nil {
			return errors.Wrap(err, "could not get consensus Message from network Message")
		}
		if err := qbftMsg.Validate(); err != nil {
			return errors.Wrap(err, "invalid qbft Message")
		}
		c.mtx.RLock()
		r, exists := c.Runners[phase0.Slot(qbftMsg.Height)]
		c.mtx.RUnlock()
		if !exists {
			return errors.New(fmt.Sprintf("no runner found for consensus message's slot(qbtf height): %d", qbftMsg.Height))
		}
		return r.ProcessConsensus(logger, msg.SignedSSVMessage)
	case spectypes.SSVPartialSignatureMsgType:
		pSigMessages := &spectypes.PartialSignatureMessages{}
		if err := pSigMessages.Decode(msg.SignedSSVMessage.SSVMessage.GetData()); err != nil {
			return errors.Wrap(err, "could not get post consensus Message from network Message")
		}

		// Validate
		if len(msg.SignedSSVMessage.OperatorIDs) != 1 {
			return errors.New("PartialSignatureMessage has more than 1 signer")
		}

		if err := pSigMessages.ValidateForSigner(msg.SignedSSVMessage.OperatorIDs[0]); err != nil {
			return errors.Wrap(err, "invalid PartialSignatureMessages")
		}

		if pSigMessages.Type == spectypes.PostConsensusPartialSig {
			c.mtx.RLock()
			r, exists := c.Runners[pSigMessages.Slot]
			c.mtx.RUnlock()
			if !exists {
				return errors.New(fmt.Sprintf("no runner found for post consensus sig message's slot: %d", pSigMessages.Slot))
			}
			return r.ProcessPostConsensus(logger, pSigMessages)
		}
	case message.SSVEventMsgType:
		return c.handleEventMessage(logger, msg)
	default:
		return errors.New("unknown msg")
	}
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
