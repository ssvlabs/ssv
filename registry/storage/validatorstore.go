package storage

import (
	"slices"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"golang.org/x/exp/maps"

	"github.com/ssvlabs/ssv/protocol/v2/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/validatorstore.go -source=./validatorstore.go

type BaseValidatorStore interface {
	Validator(pubKey []byte) *types.SSVShare
	ValidatorByIndex(index phase0.ValidatorIndex) *types.SSVShare
	Validators() []*types.SSVShare
	ParticipatingValidators(epoch phase0.Epoch) []*types.SSVShare
	OperatorValidators(id spectypes.OperatorID) []*types.SSVShare

	Committee(id spectypes.CommitteeID) *Committee
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
	byPubKey   func([]byte) *types.SSVShare

	byValidatorIndex map[phase0.ValidatorIndex]*types.SSVShare
	byCommitteeID    map[spectypes.CommitteeID]*Committee
	byOperatorID     map[spectypes.OperatorID]*sharesAndCommittees

	muValidatorIndex sync.RWMutex
	muCommitteeID    sync.RWMutex
	muOperatorID     sync.RWMutex
}

func newValidatorStore(
	shares func() []*types.SSVShare,
	shareByPubKey func([]byte) *types.SSVShare,
) *validatorStore {
	return &validatorStore{
		shares:           shares,
		byPubKey:         shareByPubKey,
		byValidatorIndex: make(map[phase0.ValidatorIndex]*types.SSVShare),
		byCommitteeID:    make(map[spectypes.CommitteeID]*Committee),
		byOperatorID:     make(map[spectypes.OperatorID]*sharesAndCommittees),
	}
}

func (c *validatorStore) Validator(pubKey []byte) *types.SSVShare {
	return c.byPubKey(pubKey)
}

func (c *validatorStore) ValidatorByIndex(index phase0.ValidatorIndex) *types.SSVShare {
	c.muValidatorIndex.RLock()
	defer c.muValidatorIndex.RUnlock()

	return c.byValidatorIndex[index]
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
	c.muOperatorID.RLock()
	defer c.muOperatorID.RUnlock()

	if data, ok := c.byOperatorID[id]; ok {
		return data.shares
	}
	return nil
}

func (c *validatorStore) Committee(id spectypes.CommitteeID) *Committee {
	c.muCommitteeID.RLock()
	defer c.muCommitteeID.RUnlock()

	return c.byCommitteeID[id]
}

func (c *validatorStore) Committees() []*Committee {
	c.muCommitteeID.RLock()
	defer c.muCommitteeID.RUnlock()

	return maps.Values(c.byCommitteeID)
}

func (c *validatorStore) ParticipatingCommittees(epoch phase0.Epoch) []*Committee {
	c.muCommitteeID.RLock()
	defer c.muCommitteeID.RUnlock()

	var committees []*Committee
	for _, committee := range c.byCommitteeID {
		if committee.IsParticipating(epoch) {
			committees = append(committees, committee)
		}
	}
	return committees
}

func (c *validatorStore) OperatorCommittees(id spectypes.OperatorID) []*Committee {
	c.muOperatorID.RLock()
	defer c.muOperatorID.RUnlock()

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

func (c *validatorStore) handleSharesAdded(shares ...*types.SSVShare) {
	for _, share := range shares {
		if share == nil {
			continue
		}
		// Update byValidatorIndex
		if share.HasBeaconMetadata() {
			c.muValidatorIndex.Lock()
			c.byValidatorIndex[share.BeaconMetadata.Index] = share
			c.muValidatorIndex.Unlock()
		}

		// Update byCommitteeID
		c.muCommitteeID.Lock()
		committee := c.byCommitteeID[share.CommitteeID()]
		if committee == nil {
			committee = buildCommittee([]*types.SSVShare{share})
			c.byCommitteeID[committee.ID] = committee
		} else {
			committee = buildCommittee(append(committee.Validators, share))
		}
		c.byCommitteeID[committee.ID] = committee
		c.muCommitteeID.Unlock()

		// Update byOperatorID
		c.muOperatorID.Lock()
		for _, operator := range share.Committee {
			data := c.byOperatorID[operator.Signer]
			if data == nil {
				data = &sharesAndCommittees{
					shares:     []*types.SSVShare{share},
					committees: []*Committee{committee},
				}
			} else {
				data.shares = append(data.shares, share)
				data.committees = append(data.committees, committee)
			}
			c.byOperatorID[operator.Signer] = data
		}
		c.muOperatorID.Unlock()
	}
}

func (c *validatorStore) handleShareRemoved(pk spectypes.ValidatorPK) {
	share := c.byPubKey(pk[:])
	if share == nil {
		return
	}

	// Update byValidatorIndex
	c.muValidatorIndex.Lock()
	delete(c.byValidatorIndex, share.BeaconMetadata.Index)
	c.muValidatorIndex.Unlock()

	// Update byCommitteeID
	c.muCommitteeID.Lock()
	committee := c.byCommitteeID[share.CommitteeID()]
	if committee == nil {
		return
	}
	validators := make([]*types.SSVShare, 0, len(committee.Validators)-1)
	indices := make([]phase0.ValidatorIndex, 0, len(committee.Validators)-1)
	for _, validator := range committee.Validators {
		if validator.ValidatorPubKey != pk {
			validators = append(validators, validator)
			indices = append(indices, validator.ValidatorIndex)
		}
	}
	if len(validators) == 0 {
		delete(c.byCommitteeID, committee.ID)
	} else {
		committee.Validators = validators
		committee.Indices = indices
	}
	c.muCommitteeID.Unlock()

	// Update byOperatorID
	c.muOperatorID.Lock()
	for _, operator := range share.Committee {
		data := c.byOperatorID[operator.Signer]
		if data == nil {
			return
		}
		shares := make([]*types.SSVShare, 0, len(data.shares)-1)
		for _, s := range data.shares {
			if s.ValidatorPubKey != pk {
				shares = append(shares, s)
			}
		}
		if len(shares) == 0 {
			delete(c.byOperatorID, operator.Signer)
		} else {
			data.shares = shares
		}
	}
	c.muOperatorID.Unlock()
}

func (c *validatorStore) handleSharesUpdated(shares ...*types.SSVShare) {
	for _, share := range shares {

		// Update byValidatorIndex
		if share.HasBeaconMetadata() {
			c.muValidatorIndex.Lock()
			c.byValidatorIndex[share.BeaconMetadata.Index] = share
			c.muValidatorIndex.Unlock()
		}

		// Update byCommitteeID
		c.muCommitteeID.RLock()
		committee := c.byCommitteeID[share.CommitteeID()]
		c.muCommitteeID.RUnlock()

		if committee != nil {
			c.muCommitteeID.Lock()
			for i, validator := range committee.Validators {
				if validator.ValidatorPubKey == share.ValidatorPubKey {
					committee.Validators[i] = share
					committee.Indices[i] = share.ValidatorIndex
					break
				}
			}
			c.muCommitteeID.Unlock()
		}

		// Update byOperatorID
		c.muOperatorID.RLock()
		for _, shareMember := range share.Committee {
			if data := c.byOperatorID[shareMember.Signer]; data != nil {
				for i, s := range data.shares {
					if s.ValidatorPubKey == share.ValidatorPubKey {
						data.shares[i] = share
						break
					}
				}
			}
		}
		c.muOperatorID.RUnlock()
	}
}

func (c *validatorStore) handleDrop() {
	// Drop byValidatorIndex
	c.muValidatorIndex.Lock()
	c.byValidatorIndex = make(map[phase0.ValidatorIndex]*types.SSVShare)
	c.muValidatorIndex.Unlock()

	// Drop byCommitteeID
	c.muCommitteeID.Lock()
	c.byCommitteeID = make(map[spectypes.CommitteeID]*Committee)
	c.muCommitteeID.Unlock()

	// Drop byOperatorID
	c.muOperatorID.Lock()
	c.byOperatorID = make(map[spectypes.OperatorID]*sharesAndCommittees)
	c.muOperatorID.Unlock()
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
