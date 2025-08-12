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

	validatorsMx sync.RWMutex
	validators   map[spectypes.ValidatorPK]*validator.Validator

	committeesMx sync.RWMutex
	committees   map[spectypes.CommitteeID]*validator.Committee
}

func New(ctx context.Context, opts ...Option) *ValidatorsMap {
	vm := &ValidatorsMap{
		ctx:          ctx,
		validatorsMx: sync.RWMutex{},
		committeesMx: sync.RWMutex{},
		validators:   make(map[spectypes.ValidatorPK]*validator.Validator),
		committees:   make(map[spectypes.CommitteeID]*validator.Committee),
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
	vm.validatorsMx.RLock()
	defer vm.validatorsMx.RUnlock()

	for _, val := range vm.validators {
		if !iterator(val) {
			return false
		}
	}
	return true
}

// GetValidator returns a validator
func (vm *ValidatorsMap) GetValidator(pubKey spectypes.ValidatorPK) (*validator.Validator, bool) {
	vm.validatorsMx.RLock()
	defer vm.validatorsMx.RUnlock()

	v, ok := vm.validators[pubKey]

	return v, ok
}

// PutValidator creates a new validator instance
func (vm *ValidatorsMap) PutValidator(pubKey spectypes.ValidatorPK, v *validator.Validator) {
	vm.validatorsMx.Lock()
	defer vm.validatorsMx.Unlock()

	vm.validators[pubKey] = v
}

// RemoveValidator removes a validator instance from the map
func (vm *ValidatorsMap) RemoveValidator(pubKey spectypes.ValidatorPK) *validator.Validator {
	if v, found := vm.GetValidator(pubKey); found {
		vm.validatorsMx.Lock()
		defer vm.validatorsMx.Unlock()

		delete(vm.validators, pubKey)
		return v
	}
	return nil
}

// SizeValidators returns the number of validators in the map
func (vm *ValidatorsMap) SizeValidators() int {
	vm.validatorsMx.RLock()
	defer vm.validatorsMx.RUnlock()

	return len(vm.validators)
}

// Committee methods

// ForEachCommittee loops over committees applying iterator func
func (vm *ValidatorsMap) ForEachCommittee(iterator committeeIterator) bool {
	vm.committeesMx.RLock()
	defer vm.committeesMx.RUnlock()

	for _, val := range vm.committees {
		if !iterator(val) {
			return false
		}
	}
	return true
}

// GetAllCommittees returns all committees.
func (vm *ValidatorsMap) GetAllCommittees() []*validator.Committee {
	vm.committeesMx.RLock()
	defer vm.committeesMx.RUnlock()

	committees := make([]*validator.Committee, 0, len(vm.committees))
	for _, val := range vm.committees {
		committees = append(committees, val)
	}

	return committees
}

// GetCommittee returns a committee
func (vm *ValidatorsMap) GetCommittee(pubKey spectypes.CommitteeID) (*validator.Committee, bool) {
	vm.committeesMx.RLock()
	defer vm.committeesMx.RUnlock()

	v, ok := vm.committees[pubKey]

	return v, ok
}

// PutCommittee creates a new committee instance
func (vm *ValidatorsMap) PutCommittee(pubKey spectypes.CommitteeID, v *validator.Committee) {
	vm.committeesMx.Lock()
	defer vm.committeesMx.Unlock()

	vm.committees[pubKey] = v
}

// RemoveCommittee removes a committee instance from the map
func (vm *ValidatorsMap) RemoveCommittee(pubKey spectypes.CommitteeID) *validator.Committee {
	if v, found := vm.GetCommittee(pubKey); found {
		vm.committeesMx.Lock()
		defer vm.committeesMx.Unlock()

		delete(vm.committees, pubKey)
		return v
	}
	return nil
}

// SizeCommittees returns the number of committees in the map
func (vm *ValidatorsMap) SizeCommittees() int {
	vm.committeesMx.RLock()
	defer vm.committeesMx.RUnlock()

	return len(vm.committees)
}
