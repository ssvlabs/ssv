package validator

// TODO(nkryuchkov): remove old validator interface(s)
import (
	"context"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/bloxapp/ssv/protocol/v2/share"
	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	"github.com/bloxapp/ssv/storage/basedb"

	"go.uber.org/zap"
)

// validatorIterator is the function used to iterate over existing validators
type validatorIterator func(validator *validator.Validator) error

// validatorsMap manages a collection of running validators
type validatorsMap struct {
	logger *zap.Logger
	ctx    context.Context
	db     basedb.IDb

	optsTemplate *validator.Options

	lock          sync.RWMutex
	validatorsMap map[string]*validator.Validator
}

func newValidatorsMap(ctx context.Context, logger *zap.Logger, db basedb.IDb, optsTemplate *validator.Options) *validatorsMap {
	vm := validatorsMap{
		logger:        logger.With(zap.String("component", "validatorsMap")),
		ctx:           ctx,
		db:            db,
		lock:          sync.RWMutex{},
		validatorsMap: make(map[string]*validator.Validator),
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
func (vm *validatorsMap) GetValidator(pubKey string) (*validator.Validator, bool) {
	// main lock
	vm.lock.RLock()
	defer vm.lock.RUnlock()

	v, ok := vm.validatorsMap[pubKey]

	return v, ok
}

// GetOrCreateValidator creates a new validator instance if not exist
func (vm *validatorsMap) GetOrCreateValidator(share *share.Share, metadata *share.Metadata) *validator.Validator {
	// main lock
	vm.lock.Lock()
	defer vm.lock.Unlock()

	pubKey := hex.EncodeToString(share.ValidatorPubKey)
	if v, ok := vm.validatorsMap[pubKey]; !ok {
		opts := *vm.optsTemplate
		opts.Share = share
		opts.Metadata = metadata
		opts.Mode = validator.ModeRW
		opts.DutyRunners = setupRunners(vm.ctx, opts)
		vm.validatorsMap[pubKey] = validator.NewValidator(vm.ctx, opts)
		printShare(share, vm.logger, "setup validator done")
		opts.Share = nil
		opts.Metadata = nil
	} else {
		printShare(v.Share, vm.logger, "get validator")
	}

	return vm.validatorsMap[pubKey]
}

// RemoveValidator removes a validator instance from the map
func (vm *validatorsMap) RemoveValidator(pubKey string) *validator.Validator {
	if v, found := vm.GetValidator(pubKey); found {
		vm.lock.Lock()
		defer vm.lock.Unlock()

		delete(vm.validatorsMap, pubKey)
		return v
	}
	return nil
}

// Size returns the number of validators in the map
func (vm *validatorsMap) Size() int {
	vm.lock.RLock()
	defer vm.lock.RUnlock()

	return len(vm.validatorsMap)
}

func printShare(s *share.Share, logger *zap.Logger, msg string) {
	var committee []string
	for _, c := range s.Committee {
		committee = append(committee, fmt.Sprintf(`[OperatorID=%d, PubKey=%x]`, c.OperatorID, c.PubKey))
	}
	logger.Debug(msg,
		zap.String("pubKey", hex.EncodeToString(s.ValidatorPubKey)),
		zap.Uint64("nodeID", uint64(s.OperatorID)),
		zap.Strings("committee", committee))
}
