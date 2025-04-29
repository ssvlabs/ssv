package storage

import (
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

//go:generate go tool -modfile=../../tool.mod mockgen -package=mocks -destination=./mocks/validatorstore.go -source=./validatorstore.go

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
func (c *Committee) IsParticipating(cfg networkconfig.NetworkConfig, epoch phase0.Epoch) bool {
	for _, validator := range c.Validators {
		if validator.IsParticipating(cfg, epoch) {
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

	networkConfig networkconfig.NetworkConfig

	mu sync.RWMutex
}

func newValidatorStore(
	shares func() []*types.SSVShare,
	shareByPubKey func([]byte) (*types.SSVShare, bool),
	networkConfig networkconfig.NetworkConfig,
) *validatorStore {
	return &validatorStore{
		shares:           shares,
		byPubKey:         shareByPubKey,
		byValidatorIndex: make(map[phase0.ValidatorIndex]*types.SSVShare),
		byCommitteeID:    make(map[spectypes.CommitteeID]*Committee),
		byOperatorID:     make(map[spectypes.OperatorID]*sharesAndCommittees),
		networkConfig:    networkConfig,
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
		if share.IsParticipating(c.networkConfig, epoch) {
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

	return slices.Collect(maps.Values(c.byCommitteeID))
}

func (c *validatorStore) ParticipatingCommittees(epoch phase0.Epoch) []*Committee {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var committees []*Committee
	for _, committee := range c.byCommitteeID {
		if committee.IsParticipating(c.networkConfig, epoch) {
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
	shares := c.OperatorValidators(c.operatorID())
	var participating []*types.SSVShare
	for _, share := range shares {
		if share.IsParticipating(c.networkConfig, epoch) {
			participating = append(participating, share)
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
		if committee.IsParticipating(c.networkConfig, epoch) {
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
			c.byValidatorIndex[share.ValidatorIndex] = share
		}

		// Update byCommitteeID
		committeeID := share.CommitteeID()
		committee, exists := c.byCommitteeID[committeeID]
		if exists {
			// Verify share does not already exist in committee.
			if containsShare(committee.Validators, share) {
				// Corrupt state.
				return fmt.Errorf("share already exists in committee. validator_pubkey=%s committee_id=%s",
					hex.EncodeToString(share.ValidatorPubKey[:]), hex.EncodeToString(committeeID[:]))
			}

			// Rebuild committee.
			committee = buildCommittee(append(committee.Validators, share))
		} else {
			// Build new committee.
			committee = buildCommittee([]*types.SSVShare{share})
		}
		c.byCommitteeID[committee.ID] = committee

		// Update byOperatorID
		seenOperators := make(map[spectypes.OperatorID]struct{})
		for _, operator := range share.Committee {
			if _, seen := seenOperators[operator.Signer]; seen {
				// Corrupt state.
				return fmt.Errorf("duplicate operator in share. operator_id=%d", operator.Signer)
			}
			seenOperators[operator.Signer] = struct{}{}

			data, exists := c.byOperatorID[operator.Signer]
			if !exists {
				data = &sharesAndCommittees{
					shares:     []*types.SSVShare{},
					committees: []*Committee{},
				}
			}

			if containsShare(data.shares, share) {
				// Corrupt state.
				return fmt.Errorf("share already exists in operator. validator_pubkey=%s operator_id=%d",
					hex.EncodeToString(share.ValidatorPubKey[:]), operator.Signer)
			}
			data.shares = append(data.shares, share)

			// Copy `committees` to avoid shared state issues.
			updated := false
			newCommittees := make([]*Committee, len(data.committees))
			copy(newCommittees, data.committees)
			for i, c := range newCommittees {
				if c.ID == committee.ID {
					newCommittees[i] = committee
					updated = true
					break
				}
			}

			if !updated {
				newCommittees = append(newCommittees, committee)
			}
			data.committees = newCommittees
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
	if share.HasBeaconMetadata() {
		delete(c.byValidatorIndex, share.ValidatorIndex)
	}

	// Update byCommitteeID
	committeeID := share.CommitteeID()
	committee, exists := c.byCommitteeID[committeeID]
	if !exists {
		return fmt.Errorf("committee not found. id=%s", hex.EncodeToString(committeeID[:]))
	}

	newCommittee, err := removeShareFromCommittee(committee, share)
	if err != nil {
		return fmt.Errorf("failed to remove share from committee. %w", err)
	}

	committeeRemoved := len(newCommittee.Validators) == 0
	if committeeRemoved {
		delete(c.byCommitteeID, newCommittee.ID)
	} else {
		c.byCommitteeID[newCommittee.ID] = newCommittee
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

		// Copy `committees` to avoid shared state issues.
		newCommittees := make([]*Committee, len(data.committees))
		copy(newCommittees, data.committees)
		for i, c := range newCommittees {
			if c.ID == committeeID {
				if committeeRemoved {
					// Remove the committee if it was completely removed
					newCommittees = append(newCommittees[:i], newCommittees[i+1:]...)
				} else {
					// Replace the committee with the updated one
					newCommittees[i] = newCommittee
				}
				break
			}
		}
		data.committees = newCommittees

		if len(data.shares) == 0 {
			delete(c.byOperatorID, operator.Signer)
		} else {
			c.byOperatorID[operator.Signer] = data
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
			c.byValidatorIndex[share.ValidatorIndex] = share
		}

		// Update byCommitteeID
		committeeID := share.CommitteeID()
		committee, exists := c.byCommitteeID[committeeID]
		if !exists {
			return fmt.Errorf("committee not found. id=%s", hex.EncodeToString(committeeID[:]))
		}

		newCommittee, err := updateCommitteeWithShare(committee, share)
		if err != nil {
			return fmt.Errorf("failed to update share in committee. %w", err)
		}
		c.byCommitteeID[committeeID] = newCommittee

		// Update byOperatorID
		for _, shareMember := range share.Committee {
			data, exists := c.byOperatorID[shareMember.Signer]
			if !exists {
				return fmt.Errorf("operator not found. operator_id=%d", shareMember.Signer)
			}

			updated := false
			for i, s := range data.shares {
				if s.ValidatorPubKey == share.ValidatorPubKey {
					data.shares[i] = share
					updated = true
					break
				}
			}
			if !updated {
				return fmt.Errorf("share not found in operator. validator_pubkey=%s operator_id=%d",
					hex.EncodeToString(share.ValidatorPubKey[:]), shareMember.Signer)
			}

			// Copy `committees` to avoid shared state issues.
			newCommittees := make([]*Committee, len(data.committees))
			copy(newCommittees, data.committees)
			for i, c := range newCommittees {
				if c.ID == committeeID {
					newCommittees[i] = newCommittee
					break
				}
			}
			data.committees = newCommittees
			c.byOperatorID[shareMember.Signer] = data
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

func removeShareFromCommittee(committee *Committee, shareToRemove *types.SSVShare) (*Committee, error) {
	var shares []*types.SSVShare
	removed := false

	for i, share := range committee.Validators {
		if share.ValidatorPubKey == shareToRemove.ValidatorPubKey {
			if share.ValidatorIndex != committee.Indices[i] {
				// Corrupt state.
				return nil, fmt.Errorf("share index mismatch. validator_pubkey=%s committee_id=%s validator_index=%d committee_index=%d",
					hex.EncodeToString(share.ValidatorPubKey[:]), hex.EncodeToString(committee.ID[:]), share.ValidatorIndex, committee.Indices[i])
			}
			removed = true
			continue // Skip adding this share
		}

		shares = append(shares, share)
	}

	if !removed {
		// Corrupt state.
		return nil, fmt.Errorf("share not found in committee. validator_pubkey=%s committee_id=%s",
			hex.EncodeToString(shareToRemove.ValidatorPubKey[:]), hex.EncodeToString(committee.ID[:]))
	}

	if len(shares) == 0 {
		return &Committee{
			ID: committee.ID,
		}, nil
	}

	return buildCommittee(shares), nil
}

func updateCommitteeWithShare(committee *Committee, shareToUpdate *types.SSVShare) (*Committee, error) {
	shares := make([]*types.SSVShare, len(committee.Validators))
	copy(shares, committee.Validators)

	updated := false
	for i, share := range shares {
		if share.ValidatorPubKey == shareToUpdate.ValidatorPubKey {
			shares[i] = shareToUpdate
			updated = true
		}
	}

	if !updated {
		return nil, fmt.Errorf("share not found in committee. validator_pubkey=%s committee_id=%s",
			hex.EncodeToString(shareToUpdate.ValidatorPubKey[:]), hex.EncodeToString(committee.ID[:]))
	}

	return buildCommittee(shares), nil
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

func buildCommittee(shares []*types.SSVShare) *Committee {
	committee := &Committee{
		ID:         shares[0].CommitteeID(),
		Operators:  make([]spectypes.OperatorID, 0, len(shares)),
		Validators: shares,
		Indices:    make([]phase0.ValidatorIndex, 0, len(shares)),
	}

	// Set operator IDs.
	for _, member := range shares[0].Committee {
		committee.Operators = append(committee.Operators, member.Signer)
	}

	// Set validator indices.
	for _, share := range shares {
		committee.Indices = append(committee.Indices, share.ValidatorIndex)
	}
	slices.Sort(committee.Operators)

	return committee
}
