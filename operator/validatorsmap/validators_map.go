package validatorsmap

// TODO(nkryuchkov): remove old validator interface(s)
import (
	"context"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"

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

// CommitteeActiveIndices fetches indices of in-committee validators who are either attesting or queued and
// whose activation epoch is not greater than the passed epoch.
func (vm *ValidatorsMap) CommitteeActiveIndices(epoch phase0.Epoch) []phase0.ValidatorIndex {
	validators := vm.GetAll()
	indices := make([]phase0.ValidatorIndex, 0, len(validators))
	for _, v := range validators {
		if v.Share.Active(epoch) {
			indices = append(indices, v.Share.BeaconMetadata.Index)
		}
	}
	return indices
}

func (vm *ValidatorsMap) GetValidator(pubKey spectypes.ValidatorPK) (*validator.Validator, bool) {
	vm.lock.RLock()
	defer vm.lock.RUnlock()

	v, ok := vm.validatorsMap[string(pubKey)]

	return v, ok
}

func (vm *ValidatorsMap) CreateValidator(pubKey spectypes.ValidatorPK, v *validator.Validator) {
	vm.lock.Lock()
	defer vm.lock.Unlock()

	vm.validatorsMap[string(pubKey)] = v
}

func (vm *ValidatorsMap) RemoveValidator(pubKey spectypes.ValidatorPK) {
	vm.lock.Lock()
	defer vm.lock.Unlock()

	delete(vm.validatorsMap, string(pubKey))
}

// Size returns the number of validators in the map
func (vm *ValidatorsMap) Size() int {
	vm.lock.RLock()
	defer vm.lock.RUnlock()

	return len(vm.validatorsMap)
}
