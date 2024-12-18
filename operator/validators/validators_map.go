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
	ctx        context.Context
	vlock      sync.RWMutex
	mlock      sync.RWMutex
	validators map[spectypes.ValidatorPK]*validator.Validator
	committees map[spectypes.CommitteeID]*validator.Committee
}

func New(ctx context.Context, opts ...Option) *ValidatorsMap {
	vm := &ValidatorsMap{
		ctx:        ctx,
		vlock:      sync.RWMutex{},
		mlock:      sync.RWMutex{},
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

// ForEach loops over validators
func (vm *ValidatorsMap) ForEachValidator(iterator validatorIterator) bool {
	vm.vlock.RLock()
	defer vm.vlock.RUnlock()

	for _, val := range vm.validators {
		if !iterator(val) {
			return false
		}
	}
	return true
}

// GetValidator returns a validator
func (vm *ValidatorsMap) GetValidator(pubKey spectypes.ValidatorPK) (*validator.Validator, bool) {
	vm.vlock.RLock()
	defer vm.vlock.RUnlock()

	v, ok := vm.validators[pubKey]

	return v, ok
}

// PutValidator creates a new validator instance
func (vm *ValidatorsMap) PutValidator(pubKey spectypes.ValidatorPK, v *validator.Validator) {
	vm.vlock.Lock()
	defer vm.vlock.Unlock()

	vm.validators[pubKey] = v
}

// Remove removes a validator instance from the map
// TODO: pass spectypes.ValidatorPK instead of string
func (vm *ValidatorsMap) RemoveValidator(pubKey spectypes.ValidatorPK) *validator.Validator {
	if v, found := vm.GetValidator(pubKey); found {
		vm.vlock.Lock()
		defer vm.vlock.Unlock()

		delete(vm.validators, pubKey)
		return v
	}
	return nil
}

// SizeValidators returns the number of validators in the map
func (vm *ValidatorsMap) SizeValidators() int {
	vm.vlock.RLock()
	defer vm.vlock.RUnlock()

	return len(vm.validators)
}

// Committee methods

// ForEach loops over committees
func (vm *ValidatorsMap) ForEachCommittee(iterator committeeIterator) bool {
	vm.mlock.RLock()
	defer vm.mlock.RUnlock()

	for _, val := range vm.committees {
		if !iterator(val) {
			return false
		}
	}
	return true
}

// GetAllCommittees returns all committees.
func (vm *ValidatorsMap) GetAllCommittees() []*validator.Committee {
	vm.mlock.RLock()
	defer vm.mlock.RUnlock()

	var committees []*validator.Committee
	for _, val := range vm.committees {
		committees = append(committees, val)
	}

	return committees
}

// GetCommittee returns a committee
func (vm *ValidatorsMap) GetCommittee(pubKey spectypes.CommitteeID) (*validator.Committee, bool) {
	vm.mlock.RLock()
	defer vm.mlock.RUnlock()

	v, ok := vm.committees[pubKey]

	return v, ok
}

// PutCommittee creates a new committee instance
func (vm *ValidatorsMap) PutCommittee(pubKey spectypes.CommitteeID, v *validator.Committee) {
	vm.mlock.Lock()
	defer vm.mlock.Unlock()

	vm.committees[pubKey] = v
}

// Remove removes a committee instance from the map
func (vm *ValidatorsMap) RemoveCommittee(pubKey spectypes.CommitteeID) *validator.Committee {
	if v, found := vm.GetCommittee(pubKey); found {
		vm.mlock.Lock()
		defer vm.mlock.Unlock()

		delete(vm.committees, pubKey)
		return v
	}
	return nil
}

// SizeCommittees returns the number of committees in the map
func (vm *ValidatorsMap) SizeCommittees() int {
	vm.mlock.RLock()
	defer vm.mlock.RUnlock()

	return len(vm.committees)
}
