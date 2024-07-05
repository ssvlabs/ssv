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
	CreateRunnerFn          func(slot phase0.Slot, shares map[phase0.ValidatorIndex]*spectypes.Share) *runner.CommitteeRunner
	HighestAttestingSlotMap map[spectypes.ValidatorPK]phase0.Slot
}

// NewCommittee creates a new cluster
func NewCommittee(
	ctx context.Context,
	logger *zap.Logger,
	beaconNetwork spectypes.BeaconNetwork,
	operator *spectypes.CommitteeMember,
	verifier spectypes.SignatureVerifier,
	createRunnerFn func(slot phase0.Slot, shares map[phase0.ValidatorIndex]*spectypes.Share) *runner.CommitteeRunner,
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
	// TODO alan : lock per slot?
	if _, exists := c.Runners[duty.Slot]; exists {
		return errors.New(fmt.Sprintf("CommitteeRunner for slot %d already exists", duty.Slot))
	}

	var validatorToStopMap map[phase0.Slot]spectypes.ValidatorPK
	//Filter old duties based on highest attesting slot

	duty, validatorToStopMap, highestAttestingSlotMap, err := FilterCommitteeDuty(logger, duty, c.HighestAttestingSlotMap)
	if err != nil {
		return errors.Wrap(err, "cannot filter committee duty")
	}
	c.HighestAttestingSlotMap = highestAttestingSlotMap
	// Stop validators with old duties
	c.stopDuties(logger, validatorToStopMap)
	c.updateAttestingSlotMap(duty)

	if len(duty.BeaconDuties) == 0 {
		logger.Debug("No beacon duties to run")
		return nil
	}

	var sharesCopy = make(map[phase0.ValidatorIndex]*spectypes.Share, len(c.Shares))
	for k, v := range c.Shares {
		sharesCopy[k] = v
	}

	r := c.CreateRunnerFn(duty.Slot, sharesCopy)
	// Set timeout function.
	r.GetBaseRunner().TimeoutF = c.onTimeout
	c.Runners[duty.Slot] = r
	if _, ok := c.Queues[duty.Slot]; !ok {
		c.Queues[duty.Slot] = queueContainer{
			Q: queue.WithMetrics(queue.New(1000), nil), // TODO alan: get queue opts from options
			queueState: &queue.State{
				HasRunningInstance: false,
				Height:             qbft.Height(duty.Slot),
				Slot:               0,
				//Quorum:             options.SSVShare.Share,// TODO
			},
		}

	}

	logger = c.logger.With(fields.DutyID(fields.FormatCommitteeDutyID(c.Operator.Committee, c.BeaconNetwork.EstimatedEpochAtSlot(duty.Slot), duty.Slot)), fields.Slot(duty.Slot))
	// TODO alan: stop queue
	go c.ConsumeQueue(logger, duty.Slot, c.ProcessMessage)

	logger.Info("ℹ️ starting duty processing")
	return c.Runners[duty.Slot].StartNewDuty(logger, duty, c.Operator.GetQuorum())
}

// NOT threadsafe
func (c *Committee) stopDuties(logger *zap.Logger, validatorToStopMap map[phase0.Slot]spectypes.ValidatorPK) {
	for slot, validator := range validatorToStopMap {
		r, exists := c.Runners[slot]
		if exists {
			logger.Debug("stopping duty for validator",
				fields.DutyID(fields.FormatCommitteeDutyID(c.Operator.Committee, c.BeaconNetwork.EstimatedEpochAtSlot(slot), slot)),
				zap.Uint64("slot", uint64(slot)), zap.String("validator", hex.EncodeToString(validator[:])),
			)
			r.StopDuty(validator)
		}
	}
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

// FilterCommitteeDuty filters the committee duties by the slots given per validator.
// It returns the filtered duties, the validators to stop and updated slot map.
// NOT threadsafe
func FilterCommitteeDuty(logger *zap.Logger, duty *spectypes.CommitteeDuty, slotMap map[spectypes.ValidatorPK]phase0.Slot) (
	*spectypes.CommitteeDuty,
	map[phase0.Slot]spectypes.ValidatorPK,
	map[spectypes.ValidatorPK]phase0.Slot,
	error,
) {
	validatorsToStop := make(map[phase0.Slot]spectypes.ValidatorPK)
	indicesToRemove := make([]int, 0)
	for i, beaconDuty := range duty.BeaconDuties {
		validatorPK := spectypes.ValidatorPK(beaconDuty.PubKey)
		slot, exists := slotMap[validatorPK]
		if exists {
			if slot < beaconDuty.Slot {
				validatorsToStop[beaconDuty.Slot] = validatorPK
				slotMap[validatorPK] = beaconDuty.Slot
			} else { // else don't run duty with old slot
				// remove the duty
				logger.Debug("removing beacon duty from committeeduty", zap.Uint64("slot", uint64(beaconDuty.Slot)), zap.String("validator", hex.EncodeToString(beaconDuty.PubKey[:])))
				indicesToRemove = append(indicesToRemove, i)
			}
		}
	}

	filteredDuties, err := removeIndices(duty.BeaconDuties, indicesToRemove)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "cannot remove indices")
	}
	duty.BeaconDuties = filteredDuties
	return duty, validatorsToStop, slotMap, err
}

// ProcessMessage processes Network Message of all types
func (c *Committee) ProcessMessage(logger *zap.Logger, msg *queue.DecodedSSVMessage) error {
	// Validate message
	if msg.GetType() != message.SSVEventMsgType {
		if err := msg.SignedSSVMessage.Validate(); err != nil {
			return errors.Wrap(err, "invalid signed message")
		}

		// Verify SignedSSVMessage's signature
		if err := c.SignatureVerifier.Verify(msg.SignedSSVMessage, c.Operator.Committee); err != nil {
			return errors.Wrap(err, "SignedSSVMessage has an invalid signature")
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
		Runners  map[phase0.Slot]*runner.CommitteeRunner
		Operator *spectypes.CommitteeMember
		Shares   map[phase0.ValidatorIndex]*spectypes.Share
	}

	// Create object and marshal
	alias := &CommitteeAlias{
		Runners:  c.Runners,
		Operator: c.Operator,
		Shares:   c.Shares,
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
