package validator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	"github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/logging/fields"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
)

type CommitteeRunnerFunc func(slot phase0.Slot, shares map[phase0.ValidatorIndex]*spectypes.Share, attestingValidators []spectypes.ShareValidatorPK) *runner.CommitteeRunner

type Committee struct {
	logger *zap.Logger
	ctx    context.Context

	mtx           sync.RWMutex
	BeaconNetwork spectypes.BeaconNetwork
	Storage       *storage.QBFTStores

	Queues map[phase0.Slot]queueContainer

	//runnersMtx sync.RWMutex
	Runners map[phase0.Slot]*runner.CommitteeRunner

	//sharesMtx sync.RWMutex
	Shares map[phase0.ValidatorIndex]*spectypes.Share

	Operator *spectypes.CommitteeMember

	SignatureVerifier       spectypes.SignatureVerifier
	CreateRunnerFn          CommitteeRunnerFunc
	HighestAttestingSlotMap map[spectypes.ValidatorPK]phase0.Slot
}

// NewCommittee creates a new cluster
func NewCommittee(
	ctx context.Context,
	logger *zap.Logger,
	beaconNetwork spectypes.BeaconNetwork,
	operator *spectypes.CommitteeMember,
	verifier spectypes.SignatureVerifier,
	createRunnerFn CommitteeRunnerFunc,
	// share map[phase0.ValidatorIndex]*spectypes.Share, // TODO Shouldn't we pass the shares map here the same way we do in spec?
) *Committee {
	return &Committee{
		logger:        logger,
		BeaconNetwork: beaconNetwork,
		ctx:           ctx,
		// TODO alan: drain maps
		Queues:  make(map[phase0.Slot]queueContainer),
		Runners: make(map[phase0.Slot]*runner.CommitteeRunner),
		Shares:  make(map[phase0.ValidatorIndex]*spectypes.Share),
		//Shares:                  share,
		HighestAttestingSlotMap: make(map[spectypes.ValidatorPK]phase0.Slot),
		Operator:                operator,
		SignatureVerifier:       verifier,
		CreateRunnerFn:          createRunnerFn,
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
		c.stopValidator(c.logger, share.ValidatorPubKey)
		delete(c.Shares, validatorIndex)
	}
}

// StartDuty starts a new duty for the given slot
func (c *Committee) StartDuty(logger *zap.Logger, duty *spectypes.CommitteeDuty) error {
	c.logger.Debug("Starting committee duty runner", zap.Uint64("slot", uint64(duty.Slot)))
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if len(duty.BeaconDuties) == 0 {
		return errors.New("no beacon duties")
	}
	if _, exists := c.Runners[duty.Slot]; exists {
		return errors.New(fmt.Sprintf("CommitteeRunner for slot %d already exists", duty.Slot))
	}

	attestingValidators := make([]spectypes.ShareValidatorPK, 0, len(duty.BeaconDuties))
	validatorShares := make(map[phase0.ValidatorIndex]*spectypes.Share, len(duty.BeaconDuties))
	toRemove := make([]int, 0)
	// Remove beacon duties that don't have a share
	for i, bd := range duty.BeaconDuties {
		share, ok := c.Shares[bd.ValidatorIndex]
		if !ok {
			toRemove = append(toRemove, i)
			continue
		}
		if bd.Type == spectypes.BNRoleAttester {
			attestingValidators = append(attestingValidators, share.SharePubKey)
		}
		validatorShares[bd.ValidatorIndex] = share
	}
	// Remove beacon duties that don't have a share
	if len(toRemove) > 0 {
		newDuties, err := removeIndices(duty.BeaconDuties, toRemove)
		if err != nil {
			logger.Warn("could not remove beacon duties", zap.Error(err), zap.Ints("indices", toRemove))
		} else {
			duty.BeaconDuties = newDuties
		}
	}

	if len(duty.BeaconDuties) == 0 {
		return errors.New("CommitteeDuty has no valid beacon duties")
	}

	r := c.CreateRunnerFn(duty.Slot, validatorShares, attestingValidators)
	// Set timeout function.
	r.GetBaseRunner().TimeoutF = c.onTimeout
	c.Runners[duty.Slot] = r
	if _, ok := c.Queues[duty.Slot]; !ok {
		c.Queues[duty.Slot] = queueContainer{
			Q: queue.WithMetrics(queue.New(1000), nil), // TODO alan: get queue opts from options
			queueState: &queue.State{
				HasRunningInstance: false,
				Height:             qbft.Height(duty.Slot),
				Slot:               duty.Slot,
				//Quorum:             options.SSVShare.Share,// TODO
			},
		}

	}

	logger = c.logger.With(fields.DutyID(fields.FormatCommitteeDutyID(c.Operator.Committee, c.BeaconNetwork.EstimatedEpochAtSlot(duty.Slot), duty.Slot)), fields.Slot(duty.Slot))
	// TODO alan: stop queue
	go c.ConsumeQueue(logger, duty.Slot, c.ProcessMessage, c.Runners[duty.Slot])

	logger.Info("ℹ️ starting duty processing")
	return c.Runners[duty.Slot].StartNewDuty(logger, duty, c.Operator.GetQuorum())
}

// NOT threadsafe
func (c *Committee) stopValidator(logger *zap.Logger, validator spectypes.ValidatorPK) {
	for slot, runner := range c.Runners {
		logger.Debug("trying to stop duty for validator",
			fields.DutyID(fields.FormatCommitteeDutyID(c.Operator.Committee, c.BeaconNetwork.EstimatedEpochAtSlot(slot), slot)),
			zap.Uint64("slot", uint64(slot)), zap.String("validator", hex.EncodeToString(validator[:])),
		)
		runner.StopDuty(validator)
	}
}

func (c *Committee) PushToQueue(slot phase0.Slot, dec *queue.DecodedSSVMessage) {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	if pushed := c.Queues[slot].Q.TryPush(dec); !pushed {
		c.logger.Warn("dropping ExecuteDuty message because the queue is full")
	}
}

func removeIndices(s []*spectypes.BeaconDuty, indicesToRemove []int) ([]*spectypes.BeaconDuty, error) {
	// Create a set to check for duplicate and invalid indices
	uniqueIndices := make(map[int]struct{}, len(indicesToRemove))
	for _, id := range indicesToRemove {
		if id < 0 || id >= len(s) {
			return s, fmt.Errorf("index %d out of range of slice with length %d", id, len(s))
		}
		if _, exists := uniqueIndices[id]; exists {
			return s, fmt.Errorf("duplicate index %d in %v", id, indicesToRemove)
		}
		uniqueIndices[id] = struct{}{}
	}

	// Create a result slice excluding marked elements
	result := make([]*spectypes.BeaconDuty, 0, len(s)-len(indicesToRemove))
	for i, item := range s {
		if _, found := uniqueIndices[i]; !found {
			result = append(result, item)
		}
	}

	return result, nil
}

// ProcessMessage processes Network Message of all types
func (c *Committee) ProcessMessage(logger *zap.Logger, msg *queue.DecodedSSVMessage) error {
	// Validate message
	if msg.GetType() != message.SSVEventMsgType {
		if err := msg.SignedSSVMessage.Validate(); err != nil {
			return errors.Wrap(err, "invalid SignedSSVMessage")
		}

		// Verify SignedSSVMessage's signature
		if err := c.SignatureVerifier.Verify(msg.SignedSSVMessage, c.Operator.Committee); err != nil {
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
		c.mtx.Lock()
		runner, exists := c.Runners[phase0.Slot(qbftMsg.Height)]
		c.mtx.Unlock()
		if !exists {
			return errors.New("no runner found for message's slot")
		}
		return runner.ProcessConsensus(logger, msg.SignedSSVMessage)
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
			c.mtx.Lock()
			runner, exists := c.Runners[pSigMessages.Slot]
			c.mtx.Unlock()
			if !exists {
				return errors.New("no runner found for message's slot")
			}
			return runner.ProcessPostConsensus(logger, pSigMessages)
		}
	case message.SSVEventMsgType:
		return c.handleEventMessage(logger, msg)
	default:
		return errors.New("unknown msg")
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
		CommitteeMember: c.Operator,
		Share:           c.Shares,
	}

	byts, err := json.Marshal(alias)

	return byts, err
}

func (c *Committee) UnmarshalJSON(data []byte) error {
	type CommitteeAlias struct {
		Runners  map[phase0.Slot]*runner.CommitteeRunner
		Operator *spectypes.CommitteeMember
		Shares   map[phase0.ValidatorIndex]*spectypes.Share
	}

	// Unmarshal the JSON data into the auxiliary struct
	aux := &CommitteeAlias{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Assign fields
	c.Runners = aux.Runners
	c.Operator = aux.Operator
	c.Shares = aux.Shares

	return nil
}

// updateAttestingSlotMap updates the highest attesting slot map from beacon duties
func (c *Committee) updateAttestingSlotMap(duty *spectypes.CommitteeDuty) {
	for _, beaconDuty := range duty.BeaconDuties {
		if beaconDuty.Type == spectypes.BNRoleAttester {
			validatorPK := spectypes.ValidatorPK(beaconDuty.PubKey)
			if _, ok := c.HighestAttestingSlotMap[validatorPK]; !ok {
				c.HighestAttestingSlotMap[validatorPK] = beaconDuty.Slot
			}
			if c.HighestAttestingSlotMap[validatorPK] < beaconDuty.Slot {
				c.HighestAttestingSlotMap[validatorPK] = beaconDuty.Slot
			}
		}
	}
}

func (c *Committee) validateMessage(msg *spectypes.SSVMessage) error {
	if !(c.Operator.CommitteeID.MessageIDBelongs(msg.GetID())) {
		return errors.New("msg ID doesn't match committee ID")
	}

	if len(msg.GetData()) == 0 {
		return errors.New("msg data is invalid")
	}

	return nil
}
