package storage

import (
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"

	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/validatorstore.go -source=./validatorstore.go

type ValidatorStore interface {
	Validator(pubKey []byte) *ssvtypes.SSVShare
	ValidatorByIndex(index phase0.ValidatorIndex) *ssvtypes.SSVShare
	Validators() []*ssvtypes.SSVShare
	ParticipatingValidators(epoch phase0.Epoch) []*ssvtypes.SSVShare
	OperatorValidators(id spectypes.OperatorID) []*ssvtypes.SSVShare

	Committee(id spectypes.ClusterID) *Committee
	Committees() []*Committee
	ParticipatingCommittees(epoch phase0.Epoch) []*Committee
	OperatorCommittees(id spectypes.OperatorID) []*Committee
}

type SelfValidatorStore interface {
	ValidatorStore

	SelfValidators() []*ssvtypes.SSVShare
	SelfParticipatingValidators(phase0.Epoch) []*ssvtypes.SSVShare

	SelfCommittees() []*Committee
	SelfParticipatingCommittees(phase0.Epoch) []*Committee
}

type Committee struct {
	ID         spectypes.ClusterID
	Operators  []spectypes.OperatorID
	Validators []*ssvtypes.SSVShare
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

func (c *Committee) HasQuorum(cnt int) bool {
	quorum, _ := ssvtypes.ComputeQuorumAndPartialQuorum(len(c.Operators))
	return uint64(cnt) >= quorum
}

type sharesAndCommittees struct {
	shares     []*ssvtypes.SSVShare
	committees []*Committee
}

type validatorStore struct {
	operatorID func() spectypes.OperatorID
	shares     func() []*ssvtypes.SSVShare
	byPubKey   func([]byte) *ssvtypes.SSVShare

	byValidatorIndex map[phase0.ValidatorIndex]*ssvtypes.SSVShare
	byCommitteeID    map[spectypes.ClusterID]*Committee
	byOperatorID     map[spectypes.OperatorID]*sharesAndCommittees

	mu sync.RWMutex
}

// TODO: fix
//func newValidatorStore(
//	operatorID func() spectypes.OperatorID,
//	shares func() []*ssvtypes.SSVShare,
//	shareByPubKey func([]byte) *ssvtypes.SSVShare,
//) *validatorStore {
//	return &validatorStore{
//		operatorID:       operatorID,
//		shares:           shares,
//		byPubKey:         shareByPubKey,
//		byValidatorIndex: make(map[phase0.ValidatorIndex]*ssvtypes.SSVShare),
//		byCommitteeID:    make(map[spectypes.ClusterID]*Committee),
//		byOperatorID:     make(map[spectypes.OperatorID]*sharesAndCommittees),
//	}
//}
//
//func (c *validatorStore) Validator(pubKey []byte) *ssvtypes.SSVShare {
//	return c.byPubKey(pubKey)
//}
//
//func (c *validatorStore) ValidatorByIndex(index phase0.ValidatorIndex) *ssvtypes.SSVShare {
//	c.mu.RLock()
//	defer c.mu.RUnlock()
//
//	return c.byValidatorIndex[index]
//}
//
//func (c *validatorStore) Validators() []*ssvtypes.SSVShare {
//	return c.shares()
//}
//
//func (c *validatorStore) ParticipatingValidators(epoch phase0.Epoch) []*ssvtypes.SSVShare {
//	c.mu.RLock()
//	defer c.mu.RUnlock()
//
//	var validators []*ssvtypes.SSVShare
//	for _, share := range c.shares() {
//		if share.IsParticipating(epoch) {
//			validators = append(validators, share)
//		}
//	}
//	return validators
//}
//
//func (c *validatorStore) OperatorValidators(id spectypes.OperatorID) []*ssvtypes.SSVShare {
//	c.mu.RLock()
//	defer c.mu.RUnlock()
//
//	if data, ok := c.byOperatorID[id]; ok {
//		return data.shares
//	}
//	return nil
//}
//
//func (c *validatorStore) Committee(id spectypes.ClusterID) *Committee {
//	c.mu.RLock()
//	defer c.mu.RUnlock()
//
//	return c.byCommitteeID[id]
//}
//
//func (c *validatorStore) Committees() []*Committee {
//	c.mu.RLock()
//	defer c.mu.RUnlock()
//
//	return maps.Values(c.byCommitteeID)
//}
//
//func (c *validatorStore) ParticipatingCommittees(epoch phase0.Epoch) []*Committee {
//	c.mu.RLock()
//	defer c.mu.RUnlock()
//
//	var committees []*Committee
//	for _, committee := range c.byCommitteeID {
//		if committee.IsParticipating(epoch) {
//			committees = append(committees, committee)
//		}
//	}
//	return committees
//}
//
//func (c *validatorStore) OperatorCommittees(id spectypes.OperatorID) []*Committee {
//	c.mu.RLock()
//	defer c.mu.RUnlock()
//
//	if data, ok := c.byOperatorID[id]; ok {
//		return data.committees
//	}
//	return nil
//}
//
//func (c *validatorStore) SelfValidators() []*ssvtypes.SSVShare {
//	return c.OperatorValidators(c.operatorID())
//}
//
//func (c *validatorStore) SelfParticipatingValidators(epoch phase0.Epoch) []*ssvtypes.SSVShare {
//	validators := c.OperatorValidators(c.operatorID())
//
//	c.mu.RLock()
//	defer c.mu.RUnlock()
//
//	var participating []*ssvtypes.SSVShare
//	for _, validator := range validators {
//		if validator.IsParticipating(epoch) {
//			participating = append(participating, validator)
//		}
//	}
//	return participating
//}
//
//func (c *validatorStore) SelfCommittees() []*Committee {
//	return c.OperatorCommittees(c.operatorID())
//}
//
//func (c *validatorStore) SelfParticipatingCommittees(epoch phase0.Epoch) []*Committee {
//	committees := c.OperatorCommittees(c.operatorID())
//
//	c.mu.RLock()
//	defer c.mu.RUnlock()
//
//	var participating []*Committee
//	for _, committee := range committees {
//		if committee.IsParticipating(epoch) {
//			participating = append(participating, committee)
//		}
//	}
//	return participating
//}
//
//func (c *validatorStore) handleShareAdded(share *ssvtypes.SSVShare) {
//	c.mu.Lock()
//	defer c.mu.Unlock()
//
//	// Update byValidatorIndex
//	if share.HasBeaconMetadata() {
//		c.byValidatorIndex[share.BeaconMetadata.Index] = share
//	}
//
//	// Update byCommitteeID
//	committee := c.byCommitteeID[share.CommitteeID()]
//	if committee == nil {
//		committee = buildCommittee([]*ssvtypes.SSVShare{share})
//		c.byCommitteeID[committee.ID] = committee
//	} else {
//		committee = buildCommittee(append(committee.Validators, share))
//	}
//	c.byCommitteeID[committee.ID] = committee
//
//	// Update byOperatorID
//	data := c.byOperatorID[share.OperatorID]
//	if data == nil {
//		data = &sharesAndCommittees{
//			shares:     []*ssvtypes.SSVShare{share},
//			committees: []*Committee{committee},
//		}
//	} else {
//		data.shares = append(data.shares, share)
//		data.committees = append(data.committees, committee)
//	}
//}
//
//func (c *validatorStore) handleShareRemoved(pk spectypes.ValidatorPK) {
//	c.mu.Lock()
//	defer c.mu.Unlock()
//
//	pubKeyStr := string(pk)
//	share := c.byPubKey(pk)
//	if share == nil {
//		return
//	}
//
//	// Update byValidatorIndex
//	delete(c.byValidatorIndex, share.BeaconMetadata.Index)
//
//	// Update byCommitteeID
//	committee := c.byCommitteeID[share.CommitteeID()]
//	if committee == nil {
//		return
//	}
//	validators := make([]*ssvtypes.SSVShare, 0, len(committee.Validators)-1)
//	for _, validator := range committee.Validators {
//		if string(validator.ValidatorPubKey) != pubKeyStr {
//			validators = append(validators, validator)
//		}
//	}
//	if len(validators) == 0 {
//		delete(c.byCommitteeID, committee.ID)
//	} else {
//		committee.Validators = validators
//	}
//
//	// Update byOperatorID
//	data := c.byOperatorID[share.OperatorID]
//	if data == nil {
//		return
//	}
//	shares := make([]*ssvtypes.SSVShare, 0, len(data.shares)-1)
//	for _, s := range data.shares {
//		if string(s.ValidatorPubKey) != pubKeyStr {
//			shares = append(shares, s)
//		}
//	}
//	if len(shares) == 0 {
//		delete(c.byOperatorID, share.OperatorID)
//	} else {
//		data.shares = shares
//	}
//}
//
//func (c *validatorStore) handleShareUpdated(share *ssvtypes.SSVShare) {
//	c.mu.Lock()
//	defer c.mu.Unlock()
//
//	// Update byValidatorIndex
//	if share.HasBeaconMetadata() {
//		c.byValidatorIndex[share.BeaconMetadata.Index] = share
//	}
//
//	// Update byCommitteeID
//	pubKeyStr := string(share.ValidatorPubKey)
//	for _, committee := range c.byCommitteeID {
//		for i, validator := range committee.Validators {
//			if string(validator.ValidatorPubKey) == pubKeyStr {
//				committee.Validators[i] = share
//				break
//			}
//		}
//	}
//
//	// Update byOperatorID
//	for _, data := range c.byOperatorID {
//		for i, s := range data.shares {
//			if string(s.ValidatorPubKey) == pubKeyStr {
//				data.shares[i] = share
//				break
//			}
//		}
//	}
//}
//
//func buildCommittee(shares []*ssvtypes.SSVShare) *Committee {
//	committee := &Committee{
//		ID:         shares[0].CommitteeID(),
//		Operators:  make([]spectypes.OperatorID, len(shares)),
//		Validators: shares,
//	}
//	for i, share := range shares {
//		committee.Operators[i] = share.OperatorID
//	}
//	return committee
//}
