package validators

// TODO(nkryuchkov): remove old validator interface(s)
import (
	"context"
	"github.com/bloxapp/ssv-spec/types"
	"sync"
)

// TODO: use queues

// validatorIterator is the function used to iterate over existing validators
type validatorIterator func(validator ValidatorOrCommittee) bool

type ValidatorOrCommittee interface {
	ProcessMessage(signedSSVMessage *types.SignedSSVMessage)
}

// ValidatorsMap manages a collection of running validators
type ValidatorsMap struct {
	ctx           context.Context
	lock          sync.RWMutex
	validatorsMap map[string]ValidatorOrCommittee
}

func New(ctx context.Context, opts ...Option) *ValidatorsMap {
	vm := &ValidatorsMap{
		ctx:           ctx,
		lock:          sync.RWMutex{},
		validatorsMap: make(map[string]ValidatorOrCommittee),
	}

	for _, opt := range opts {
		opt(vm)
	}

	return vm
}

// Option defines EventSyncer configuration option.
type Option func(*ValidatorsMap)

// WithInitialState sets initial state
func WithInitialState(state map[string]ValidatorOrCommittee) Option {
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
func (vm *ValidatorsMap) GetAll() []ValidatorOrCommittee {
	vm.lock.RLock()
	defer vm.lock.RUnlock()

	var validators []ValidatorOrCommittee
	for _, val := range vm.validatorsMap {
		validators = append(validators, val)
	}

	return validators
}

// Get returns a validator
// TODO: pass spectypes.ValidatorPK instead of string
func (vm *ValidatorsMap) Get(pubKey string) (ValidatorOrCommittee, bool) {
	vm.lock.RLock()
	defer vm.lock.RUnlock()

	v, ok := vm.validatorsMap[pubKey]

	return v, ok
}

// Create creates a new validator instance
// TODO: pass spectypes.ValidatorPK instead of string
func (vm *ValidatorsMap) Create(pubKey string, v ValidatorOrCommittee) {
	vm.lock.Lock()
	defer vm.lock.Unlock()

	vm.validatorsMap[pubKey] = v
}

// Remove removes a validator instance from the map
// TODO: pass spectypes.ValidatorPK instead of string
func (vm *ValidatorsMap) Remove(pubKey string) ValidatorOrCommittee {
	if v, found := vm.Get(pubKey); found {
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
