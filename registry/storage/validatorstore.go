package storage

import (
	"encoding/hex"
	"fmt"
	"slices"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"golang.org/x/exp/maps"

	"github.com/ssvlabs/ssv/protocol/v2/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/validatorstore.go -source=./validatorstore.go

type BaseValidatorStore interface {
	Validator(pubKey []byte) (*types.SSVShare, bool)
	ValidatorByIndex(index phase0.ValidatorIndex) (*types.SSVShare, bool)
	Validators() []*types.SSVShare
	ParticipatingValidators(epoch phase0.Epoch) []*types.SSVShare
	OperatorValidators(id spectypes.OperatorID) []*types.SSVShare

	Committee(id spectypes.CommitteeID) (*Committee, bool)
	Committees() []*Committee
	ParticipatingCommittees(epoch phase0.Epoch) []*Committee
	OperatorCommittees(id spectypes.OperatorID) []*Committee

	// TODO: save recipient address
}

type ValidatorStore interface {
	BaseValidatorStore

	WithOperatorID(operatorID func() spectypes.OperatorID) SelfValidatorStore
}

type SelfValidatorStore interface {
	BaseValidatorStore

	SelfValidators() []*types.SSVShare
	SelfParticipatingValidators(phase0.Epoch) []*types.SSVShare

	SelfCommittees() []*Committee
	SelfParticipatingCommittees(phase0.Epoch) []*Committee
}

type Committee struct {
	ID         spectypes.CommitteeID
	Operators  []spectypes.OperatorID
	Validators []*types.SSVShare
	Indices    []phase0.ValidatorIndex
}

// IsParticipating returns whether any validator in the committee should participate in the given epoch.
func (c *Committee) IsParticipating(epoch phase0.Epoch) bool {
	for _, validator := range c.Validators {
		if validator.IsParticipating(epoch) {
			return true
		}
	}
	return false
}

type sharesAndCommittees struct {
	shares     []*types.SSVShare
	committees []*Committee
}

type validatorStore struct {
	operatorID func() spectypes.OperatorID
	shares     func() []*types.SSVShare
	byPubKey   func([]byte) (*types.SSVShare, bool)

	byValidatorIndex map[phase0.ValidatorIndex]*types.SSVShare
	byCommitteeID    map[spectypes.CommitteeID]*Committee
	byOperatorID     map[spectypes.OperatorID]*sharesAndCommittees

	mu sync.RWMutex
}

func newValidatorStore(
	shares func() []*types.SSVShare,
	shareByPubKey func([]byte) (*types.SSVShare, bool),
) *validatorStore {
	return &validatorStore{
		shares:           shares,
		byPubKey:         shareByPubKey,
		byValidatorIndex: make(map[phase0.ValidatorIndex]*types.SSVShare),
		byCommitteeID:    make(map[spectypes.CommitteeID]*Committee),
		byOperatorID:     make(map[spectypes.OperatorID]*sharesAndCommittees),
	}
}

func (c *validatorStore) Validator(pubKey []byte) (*types.SSVShare, bool) {
	return c.byPubKey(pubKey)
}

func (c *validatorStore) ValidatorByIndex(index phase0.ValidatorIndex) (*types.SSVShare, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	share := c.byValidatorIndex[index]
	if share == nil {
		return nil, false
	}

	return share, true
}

func (c *validatorStore) Validators() []*types.SSVShare {
	return c.shares()
}

func (c *validatorStore) ParticipatingValidators(epoch phase0.Epoch) []*types.SSVShare {
	var validators []*types.SSVShare
	for _, share := range c.shares() {
		if share.IsParticipating(epoch) {
			validators = append(validators, share)
		}
	}
	return validators
}

func (c *validatorStore) OperatorValidators(id spectypes.OperatorID) []*types.SSVShare {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if data, ok := c.byOperatorID[id]; ok {
		return data.shares
	}
	return nil
}

func (c *validatorStore) Committee(id spectypes.CommitteeID) (*Committee, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	committee := c.byCommitteeID[id]
	if committee == nil {
		return nil, false
	}

	return committee, true
}

func (c *validatorStore) Committees() []*Committee {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return maps.Values(c.byCommitteeID)
}

func (c *validatorStore) ParticipatingCommittees(epoch phase0.Epoch) []*Committee {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var committees []*Committee
	for _, committee := range c.byCommitteeID {
		if committee.IsParticipating(epoch) {
			committees = append(committees, committee)
		}
	}
	return committees
}

func (c *validatorStore) OperatorCommittees(id spectypes.OperatorID) []*Committee {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if data, ok := c.byOperatorID[id]; ok {
		return data.committees
	}
	return nil
}

func (c *validatorStore) WithOperatorID(operatorID func() spectypes.OperatorID) SelfValidatorStore {
	c.operatorID = operatorID
	return c
}

func (c *validatorStore) SelfValidators() []*types.SSVShare {
	if c.operatorID == nil {
		return nil
	}
	return c.OperatorValidators(c.operatorID())
}

func (c *validatorStore) SelfParticipatingValidators(epoch phase0.Epoch) []*types.SSVShare {
	if c.operatorID == nil {
		return nil
	}
	validators := c.OperatorValidators(c.operatorID())
	var participating []*types.SSVShare
	for _, validator := range validators {
		if validator.IsParticipating(epoch) {
			participating = append(participating, validator)
		}
	}
	return participating
}

func (c *validatorStore) SelfCommittees() []*Committee {
	if c.operatorID == nil {
		return nil
	}
	return c.OperatorCommittees(c.operatorID())
}

func (c *validatorStore) SelfParticipatingCommittees(epoch phase0.Epoch) []*Committee {
	if c.operatorID == nil {
		return nil
	}
	committees := c.OperatorCommittees(c.operatorID())
	var participating []*Committee
	for _, committee := range committees {
		if committee.IsParticipating(epoch) {
			participating = append(participating, committee)
		}
	}
	return participating
}

func (c *validatorStore) handleSharesAdded(shares ...*types.SSVShare) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, share := range shares {
		if share == nil {
			return fmt.Errorf("nil share")
		}

		// Update byValidatorIndex
		if share.HasBeaconMetadata() {
			c.byValidatorIndex[share.BeaconMetadata.Index] = share
		}

		// Update byCommitteeID
		committeeID := share.CommitteeID()
		committee, exists := c.byCommitteeID[committeeID]

		if !exists {
			committee = &Committee{
				ID:         committeeID,
				Operators:  []spectypes.OperatorID{},
				Validators: []*types.SSVShare{},
				Indices:    []phase0.ValidatorIndex{},
			}
		}

		if !containsShare(committee.Validators, share) {
			committee.Validators = append(committee.Validators, share)
			committee.Indices = append(committee.Indices, share.ValidatorIndex)
		}

		addNewCommitteeOperators(committee, share.Committee)
		c.byCommitteeID[committee.ID] = committee

		// Update byOperatorID
		for _, operator := range share.Committee {
			data, exists := c.byOperatorID[operator.Signer]
			if !exists {
				data = &sharesAndCommittees{
					shares:     []*types.SSVShare{},
					committees: []*Committee{},
				}
			}

			data.shares = append(data.shares, share)

			if !containsCommittee(data.committees, committee.ID) {
				data.committees = append(data.committees, committee)
			}

			c.byOperatorID[operator.Signer] = data
		}
	}

	return nil
}

func (c *validatorStore) handleShareRemoved(share *types.SSVShare) error {
	if share == nil {
		return fmt.Errorf("nil share")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Update byValidatorIndex
	if share.BeaconMetadata != nil {
		delete(c.byValidatorIndex, share.BeaconMetadata.Index)
	}

	// Update byCommitteeID
	committeeID := share.CommitteeID()
	committee, exists := c.byCommitteeID[committeeID]
	if !exists {
		return fmt.Errorf("committee not found. id=%s", hex.EncodeToString(committeeID[:]))
	}

	if !removeShareFromCommittee(committee, share) {
		return fmt.Errorf("share not found in committee. validator_pubkey=%s committee_id=%s",
			hex.EncodeToString(share.ValidatorPubKey[:]), hex.EncodeToString(committeeID[:]))
	}

	committeeRemoved := len(committee.Validators) == 0
	if committeeRemoved {
		delete(c.byCommitteeID, committee.ID)
	}

	// Update byOperatorID
	for _, operator := range share.Committee {
		data, exists := c.byOperatorID[operator.Signer]
		if !exists {
			return fmt.Errorf("operator not found. operator_id=%d", operator.Signer)
		}

		if !removeShareFromOperator(data, share) {
			return fmt.Errorf("share not found in operator. validator_pubkey=%s operator_id=%d",
				hex.EncodeToString(share.ValidatorPubKey[:]), operator.Signer)
		}

		if committeeRemoved {
			if !removeCommitteeFromOperator(data, committee.ID) {
				return fmt.Errorf("committee not found in operator. committee_id=%s operator_id=%d",
					hex.EncodeToString(committeeID[:]), operator.Signer)
			}
		}

		if len(data.shares) == 0 {
			delete(c.byOperatorID, operator.Signer)
		}
	}

	return nil
}

func (c *validatorStore) handleSharesUpdated(shares ...*types.SSVShare) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, share := range shares {
		if share == nil {
			return fmt.Errorf("nil share")
		}

		// Update byValidatorIndex
		if share.HasBeaconMetadata() {
			c.byValidatorIndex[share.BeaconMetadata.Index] = share
		}

		// Update byCommitteeID
		committeeID := share.CommitteeID()
		committee, exists := c.byCommitteeID[committeeID]
		if !exists {
			return fmt.Errorf("committee not found. id=%s", hex.EncodeToString(committeeID[:]))
		}

		shareUpdated := false
		for i, validator := range committee.Validators {
			if validator.ValidatorPubKey == share.ValidatorPubKey {
				committee.Validators[i] = share
				committee.Indices[i] = share.ValidatorIndex
				shareUpdated = true
				break
			}
		}
		if !shareUpdated {
			return fmt.Errorf("share not found in committee. validator_pubkey=%s committee_id=%s",
				hex.EncodeToString(share.ValidatorPubKey[:]), hex.EncodeToString(committeeID[:]))
		}

		// Update byOperatorID
		for _, shareMember := range share.Committee {
			data, exists := c.byOperatorID[shareMember.Signer]
			if !exists {
				return fmt.Errorf("operator not found. operator_id=%d", shareMember.Signer)
			}

			shareUpdated = false
			for i, s := range data.shares {
				if s.ValidatorPubKey == share.ValidatorPubKey {
					data.shares[i] = share
					shareUpdated = true
					break
				}
			}
			if !shareUpdated {
				return fmt.Errorf("share not found in operator. validator_pubkey=%s operator_id=%d",
					hex.EncodeToString(share.ValidatorPubKey[:]), shareMember.Signer)
			}
		}
	}

	return nil
}

func (c *validatorStore) handleDrop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.byValidatorIndex = make(map[phase0.ValidatorIndex]*types.SSVShare)
	c.byCommitteeID = make(map[spectypes.CommitteeID]*Committee)
	c.byOperatorID = make(map[spectypes.OperatorID]*sharesAndCommittees)
}

func containsShare(shares []*types.SSVShare, share *types.SSVShare) bool {
	for _, existing := range shares {
		if existing.ValidatorPubKey == share.ValidatorPubKey {
			return true
		}
	}
	return false
}

func containsCommittee(committees []*Committee, committeeID spectypes.CommitteeID) bool {
	for _, committee := range committees {
		if committee.ID == committeeID {
			return true
		}
	}
	return false
}

func addNewCommitteeOperators(committee *Committee, shareMembers []*spectypes.ShareMember) {
	seen := make(map[spectypes.OperatorID]struct{}, len(committee.Operators))
	for _, opID := range committee.Operators {
		seen[opID] = struct{}{}
	}

	for _, member := range shareMembers {
		if _, exists := seen[member.Signer]; !exists {
			committee.Operators = append(committee.Operators, member.Signer)
			seen[member.Signer] = struct{}{}
		}
	}
	slices.Sort(committee.Operators)
}

func removeShareFromCommittee(committee *Committee, share *types.SSVShare) (found bool) {
	for i, validator := range committee.Validators {
		if validator.ValidatorPubKey == share.ValidatorPubKey {
			committee.Validators = append(committee.Validators[:i], committee.Validators[i+1:]...)
			committee.Indices = append(committee.Indices[:i], committee.Indices[i+1:]...)
			return true
		}
	}
	return false
}

func removeShareFromOperator(data *sharesAndCommittees, share *types.SSVShare) (found bool) {
	for i, s := range data.shares {
		if s.ValidatorPubKey == share.ValidatorPubKey {
			data.shares = append(data.shares[:i], data.shares[i+1:]...)
			return true
		}
	}
	return false
}

func removeCommitteeFromOperator(data *sharesAndCommittees, committeeID spectypes.CommitteeID) (found bool) {
	for i, committee := range data.committees {
		if committee.ID == committeeID {
			data.committees = append(data.committees[:i], data.committees[i+1:]...)
			return true
		}
	}
	return false
}

func buildCommittee(shares []*types.SSVShare) *Committee {
	committee := &Committee{
		ID:         shares[0].CommitteeID(),
		Operators:  make([]spectypes.OperatorID, 0, len(shares)),
		Validators: shares,
		Indices:    make([]phase0.ValidatorIndex, 0, len(shares)),
	}

	seenOperators := make(map[spectypes.OperatorID]struct{})

	for _, share := range shares {
		for _, shareMember := range share.Committee {
			seenOperators[shareMember.Signer] = struct{}{}
		}
		committee.Indices = append(committee.Indices, share.ValidatorIndex)
	}

	committee.Operators = maps.Keys(seenOperators)
	slices.Sort(committee.Operators)

	return committee
}
