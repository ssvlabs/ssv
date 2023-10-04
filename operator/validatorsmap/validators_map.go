package validatorsmap

// TODO(nkryuchkov): remove old validator interface(s)
import (
	"context"
	"sync"

	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
)

// validatorIterator is the function used to iterate over existing validators
type validatorIterator func(validator *validator.Validator) bool

// ValidatorsMap manages a collection of running validators
type ValidatorsMap struct {
	ctx           context.Context
	lock          sync.RWMutex
	validatorsMap map[string]*validator.Validator
}

func New(ctx context.Context, opts ...Option) *ValidatorsMap {
	vm := &ValidatorsMap{
		ctx:           ctx,
		lock:          sync.RWMutex{},
		validatorsMap: make(map[string]*validator.Validator),
	}

	for _, opt := range opts {
		opt(vm)
	}

	return vm
}

// Option defines EventSyncer configuration option.
type Option func(*ValidatorsMap)

// WithInitialState sets initial state
func WithInitialState(state map[string]*validator.Validator) Option {
	return func(vm *ValidatorsMap) {
		vm.validatorsMap = state
	}
}

// ForEach loops over validators
func (vm *ValidatorsMap) ForEach(iterator validatorIterator) bool {
	vm.lock.RLock()
	defer vm.lock.RUnlock()

	for _, val := range vm.validatorsMap {
		if !iterator(val) {
			return false
		}
	}
	return true
}

// GetAll returns all validators.
func (vm *ValidatorsMap) GetAll() []*validator.Validator {
	vm.lock.RLock()
	defer vm.lock.RUnlock()

	var validators []*validator.Validator
	for _, val := range vm.validatorsMap {
		validators = append(validators, val)
	}

	return validators
}

// GetValidator returns a validator
// TODO: pass spectypes.ValidatorPK instead of string
func (vm *ValidatorsMap) GetValidator(pubKey string) (*validator.Validator, bool) {
	vm.lock.RLock()
	defer vm.lock.RUnlock()

	v, ok := vm.validatorsMap[pubKey]

	return v, ok
}

// CreateValidator creates a new validator instance
// TODO: pass spectypes.ValidatorPK instead of string
func (vm *ValidatorsMap) CreateValidator(pubKey string, v *validator.Validator) {
	vm.lock.Lock()
	defer vm.lock.Unlock()

	vm.validatorsMap[pubKey] = v
}

// RemoveValidator removes a validator instance from the map
// TODO: pass spectypes.ValidatorPK instead of string
func (vm *ValidatorsMap) RemoveValidator(pubKey string) *validator.Validator {
	if v, found := vm.GetValidator(pubKey); found {
		vm.lock.Lock()
		defer vm.lock.Unlock()

		delete(vm.validatorsMap, pubKey)
		return v
	}
	return nil
}

// Size returns the number of validators in the map
func (vm *ValidatorsMap) Size() int {
	vm.lock.RLock()
	defer vm.lock.RUnlock()

	return len(vm.validatorsMap)
}
