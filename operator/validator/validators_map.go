package validator

// TODO(nkryuchkov): remove old validator interface(s)
import (
	"context"
	"encoding/hex"
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

// validatorIterator is the function used to iterate over existing validators
type validatorIterator func(validator *validator.Validator) error

// validatorsMap manages a collection of running validators
type validatorsMap struct {
	ctx           context.Context
	lock          sync.RWMutex
	validatorsMap map[string]*validator.Validator
}

func newValidatorsMap(ctx context.Context) *validatorsMap {
	vm := validatorsMap{
		ctx:           ctx,
		lock:          sync.RWMutex{},
		validatorsMap: make(map[string]*validator.Validator),
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
// TODO: pass spectypes.ValidatorPK instead of string
func (vm *validatorsMap) GetValidator(pubKey string) (*validator.Validator, bool) {
	// main lock
	vm.lock.RLock()
	defer vm.lock.RUnlock()

	v, ok := vm.validatorsMap[pubKey]

	return v, ok
}

// CreateValidator creates a new validator instance
func (vm *validatorsMap) CreateValidator(logger *zap.Logger, share *types.SSVShare, optsTemplate validator.Options) (*validator.Validator, error) {
	// main lock
	vm.lock.Lock()
	defer vm.lock.Unlock()

	pubKey := hex.EncodeToString(share.ValidatorPubKey)
	if v, ok := vm.validatorsMap[pubKey]; !ok {
		if !share.HasBeaconMetadata() {
			return nil, fmt.Errorf("beacon metadata is missing")
		}
		opts := optsTemplate
		opts.SSVShare = share

		// Share context with both the validator and the runners,
		// so that when the validator is stopped, the runners are stopped as well.
		ctx, cancel := context.WithCancel(vm.ctx)
		opts.DutyRunners = SetupRunners(ctx, logger, opts)
		opts.MessageValidator = optsTemplate.MessageValidator
		opts.Metrics = optsTemplate.Metrics
		vm.validatorsMap[pubKey] = validator.NewValidator(ctx, cancel, opts)

		printShare(share, logger, "setup validator done")
		opts.SSVShare = nil
	} else {
		printShare(v.Share, logger, "get validator")
	}

	return vm.validatorsMap[pubKey], nil
}

// RemoveValidator removes a validator instance from the map
// TODO: pass spectypes.ValidatorPK instead of string
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

func printShare(s *types.SSVShare, logger *zap.Logger, msg string) {
	committee := make([]string, len(s.Committee))
	for i, c := range s.Committee {
		committee[i] = fmt.Sprintf(`[OperatorID=%d, PubKey=%x]`, c.OperatorID, c.PubKey)
	}
	logger.Debug(msg,
		fields.PubKey(s.ValidatorPubKey),
		zap.Uint64("node_id", s.OperatorID),
		zap.Strings("committee", committee),
		fields.FeeRecipient(s.FeeRecipientAddress[:]),
	)
}
