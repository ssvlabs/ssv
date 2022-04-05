package validator

// TODO(nkryuchkov): remove old validator interface(s)
import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"

	validator2 "github.com/bloxapp/ssv/protocol/v1/keymanager"
	"github.com/bloxapp/ssv/protocol/v1/validator"
)

// validatorIterator is the function used to iterate over existing validators
type validatorIterator func(validator.Validator) error

// validatorsMap manages a collection of running validators
type validatorsMap struct {
	logger *zap.Logger
	ctx    context.Context

	optsTemplate *validator.Options

	lock          sync.RWMutex
	validatorsMap map[string]validator.Validator
}

func newValidatorsMap(ctx context.Context, logger *zap.Logger, optsTemplate *validator.Options) *validatorsMap {
	vm := validatorsMap{
		logger:        logger.With(zap.String("component", "validatorsMap")),
		ctx:           ctx,
		lock:          sync.RWMutex{},
		validatorsMap: make(map[string]validator.Validator),
		optsTemplate:  optsTemplate,
	}

	return &vm
}

// ForEach loops over validators
func (vm *validatorsMap) ForEach(iterator validatorIterator) error {
	vm.lock.RLock()
	defer vm.lock.RUnlock()

	for _, val := range vm.validatorsMap {
		if err := iterator(val); err != nil {
			return err
		}
	}
	return nil
}

// GetValidator returns a validator
func (vm *validatorsMap) GetValidator(pubKey string) (validator.Validator, bool) {
	// main lock
	vm.lock.RLock()
	defer vm.lock.RUnlock()

	v, ok := vm.validatorsMap[pubKey]

	return v, ok
}

// GetOrCreateValidator creates a new validator instance if not exist
func (vm *validatorsMap) GetOrCreateValidator(share *validator2.Share) validator.Validator {
	// main lock
	vm.lock.Lock()
	defer vm.lock.Unlock()

	pubKey := share.PublicKey.SerializeToHexStr()
	if v, ok := vm.validatorsMap[pubKey]; !ok {
		opts := *vm.optsTemplate
		opts.Share = share
		vm.validatorsMap[pubKey] = validator.NewValidator(&opts)
		printShare(share, vm.logger, "setup validator done")
		opts.Share = nil
	} else {
		printShare(v.GetShare(), vm.logger, "get validator")
	}

	return vm.validatorsMap[pubKey]
}

// Size returns the number of validators in the map
func (vm *validatorsMap) Size() int {
	vm.lock.RLock()
	defer vm.lock.RUnlock()

	return len(vm.validatorsMap)
}

func printShare(s *validator2.Share, logger *zap.Logger, msg string) {
	var committee []string
	for _, c := range s.Committee {
		committee = append(committee, fmt.Sprintf(`[IbftId=%d, PK=%x]`, c.IbftId, c.Pk))
	}
	logger.Debug(msg,
		zap.String("pubKey", s.PublicKey.SerializeToHexStr()),
		zap.Uint64("nodeID", s.NodeID),
		zap.Strings("committee", committee))
}
