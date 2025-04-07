package validators

// TODO(nkryuchkov): remove old validator interface(s)
import (
	"context"
	"sync"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
)

// TODO: use queues

// validatorIterator is the function used to iterate over existing validators
type validatorIterator func(validator *validator.Validator) bool
type committeeIterator func(validator *validator.Committee) bool

// ValidatorsMap manages a collection of running validators
type ValidatorsMap struct {
	ctx context.Context

	vLock      sync.RWMutex
	validators map[spectypes.ValidatorPK]*validator.Validator

	cLock      sync.RWMutex
	committees map[spectypes.CommitteeID]*validator.Committee
}

func New(ctx context.Context, opts ...Option) *ValidatorsMap {
	vm := &ValidatorsMap{
		ctx:        ctx,
		vLock:      sync.RWMutex{},
		cLock:      sync.RWMutex{},
		validators: make(map[spectypes.ValidatorPK]*validator.Validator),
		committees: make(map[spectypes.CommitteeID]*validator.Committee),
	}

	for _, opt := range opts {
		opt(vm)
	}

	return vm
}

// Option defines EventSyncer configuration option.
type Option func(*ValidatorsMap)

// WithInitialState sets initial state
func WithInitialState(vstate map[spectypes.ValidatorPK]*validator.Validator, mstate map[spectypes.CommitteeID]*validator.Committee) Option {
	return func(vm *ValidatorsMap) {
		vm.validators = vstate
		vm.committees = mstate
	}
}

// ForEachValidator loops over validators applying iterator func
func (vm *ValidatorsMap) ForEachValidator(iterator validatorIterator) bool {
	vm.vLock.RLock()
	defer vm.vLock.RUnlock()

	for _, val := range vm.validators {
		if !iterator(val) {
			return false
		}
	}
	return true
}

// GetValidator returns a validator
func (vm *ValidatorsMap) GetValidator(pubKey spectypes.ValidatorPK) (*validator.Validator, bool) {
	vm.vLock.RLock()
	defer vm.vLock.RUnlock()

	v, ok := vm.validators[pubKey]

	return v, ok
}

// PutValidator creates a new validator instance
func (vm *ValidatorsMap) PutValidator(pubKey spectypes.ValidatorPK, v *validator.Validator) {
	vm.vLock.Lock()
	defer vm.vLock.Unlock()

	vm.validators[pubKey] = v
}

// RemoveValidator removes a validator instance from the map
func (vm *ValidatorsMap) RemoveValidator(pubKey spectypes.ValidatorPK) *validator.Validator {
	if v, found := vm.GetValidator(pubKey); found {
		vm.vLock.Lock()
		defer vm.vLock.Unlock()

		delete(vm.validators, pubKey)
		return v
	}
	return nil
}

// SizeValidators returns the number of validators in the map
func (vm *ValidatorsMap) SizeValidators() int {
	vm.vLock.RLock()
	defer vm.vLock.RUnlock()

	return len(vm.validators)
}

// Committee methods

// ForEachCommittee loops over committees applying iterator func
func (vm *ValidatorsMap) ForEachCommittee(iterator committeeIterator) bool {
	vm.cLock.RLock()
	defer vm.cLock.RUnlock()

	for _, val := range vm.committees {
		if !iterator(val) {
			return false
		}
	}
	return true
}

// GetAllCommittees returns all committees.
func (vm *ValidatorsMap) GetAllCommittees() []*validator.Committee {
	vm.cLock.RLock()
	defer vm.cLock.RUnlock()

	var committees []*validator.Committee
	for _, val := range vm.committees {
		committees = append(committees, val)
	}

	return committees
}

// GetCommittee returns a committee
func (vm *ValidatorsMap) GetCommittee(pubKey spectypes.CommitteeID) (*validator.Committee, bool) {
	vm.cLock.RLock()
	defer vm.cLock.RUnlock()

	v, ok := vm.committees[pubKey]

	return v, ok
}

// PutCommittee creates a new committee instance
func (vm *ValidatorsMap) PutCommittee(pubKey spectypes.CommitteeID, v *validator.Committee) {
	vm.cLock.Lock()
	defer vm.cLock.Unlock()

	vm.committees[pubKey] = v
}

// RemoveCommittee removes a committee instance from the map
func (vm *ValidatorsMap) RemoveCommittee(pubKey spectypes.CommitteeID) *validator.Committee {
	if v, found := vm.GetCommittee(pubKey); found {
		vm.cLock.Lock()
		defer vm.cLock.Unlock()

		delete(vm.committees, pubKey)
		return v
	}
	return nil
}

// SizeCommittees returns the number of committees in the map
func (vm *ValidatorsMap) SizeCommittees() int {
	vm.cLock.RLock()
	defer vm.cLock.RUnlock()

	return len(vm.committees)
}
