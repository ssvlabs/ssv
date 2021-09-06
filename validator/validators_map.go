package validator

import (
	"context"
	"fmt"
	"github.com/bloxapp/ssv/validator/storage"
	"go.uber.org/zap"
	"sync"
)

// validatorIterator is the function used to iterate over existing validators
type validatorIterator func(*Validator) error

// validatorsMap manages a collection of running validators
type validatorsMap struct {
	logger *zap.Logger
	ctx    context.Context

	optsTemplate *Options

	lock          sync.RWMutex
	validatorsMap map[string]*Validator
}

func newValidatorsMap(ctx context.Context, logger *zap.Logger, optsTemplate *Options) *validatorsMap {
	vm := validatorsMap{
		logger:        logger.With(zap.String("component", "validatorsMap")),
		ctx:           ctx,
		lock:          sync.RWMutex{},
		validatorsMap: make(map[string]*Validator),
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
func (vm *validatorsMap) GetValidator(pubKey string) (*Validator, bool) {
	// main lock
	vm.lock.RLock()
	defer vm.lock.RUnlock()

	v, ok := vm.validatorsMap[pubKey]

	return v, ok
}

// GetOrCreateValidator creates a new validator instance if not exist
func (vm *validatorsMap) GetOrCreateValidator(share *storage.Share) *Validator {
	// main lock
	vm.lock.Lock()
	defer vm.lock.Unlock()

	pubKey := share.PublicKey.SerializeToHexStr()
	if v, ok := vm.validatorsMap[pubKey]; !ok {
		opts := *vm.optsTemplate
		opts.Share = share
		vm.validatorsMap[pubKey] = New(opts)
		printShare(share, vm.logger, "setup validator done")
		opts.Share = nil
	} else {
		printShare(v.Share, vm.logger, "get validator")
	}

	return vm.validatorsMap[pubKey]
}

// Size returns the number of validators in the map
func (vm *validatorsMap) Size() int {
	return len(vm.validatorsMap)
}

func printShare(s *storage.Share, logger *zap.Logger, msg string) {
	var committee []string
	for _, c := range s.Committee {
		committee = append(committee, fmt.Sprintf(`[IbftId=%d, PK=%x]`, c.IbftId, c.Pk))
	}
	logger.Debug(msg,
		zap.String("pubKey", s.PublicKey.SerializeToHexStr()),
		zap.Uint64("nodeID", s.NodeID),
		zap.Strings("committee", committee))
}
