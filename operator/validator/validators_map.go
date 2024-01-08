package validator

// TODO(nkryuchkov): remove old validator interface(s)
import (
	"context"
	"encoding/hex"
	"fmt"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	"sync"

	"github.com/bloxapp/ssv/logging/fields"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

type Validator interface {
	GetShare() *types.SSVShare
	StartDuty(logger *zap.Logger, duty *spectypes.Duty) error
	ProcessMessage(logger *zap.Logger, msg *queue.DecodedSSVMessage) error
}

// validatorIterator is the function used to iterate over existing validators
type validatorIterator func(validator *validator.Validator) error

// validatorsMap manages a collection of running validators
type validatorsMap struct {
	ctx context.Context

	optsTemplate *validator.Options

	lock                sync.RWMutex
	validatorsMap       map[string]*validator.Validator
	createValidatorFunc func(logger *zap.Logger, share *types.SSVShare) *validator.Validator
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

func (vm *validatorsMap) createValidator(logger *zap.Logger, share *types.SSVShare) *validator.Validator {
	if vm.createValidatorFunc != nil {
		return vm.createValidatorFunc(logger, share)
	}
	opts := *vm.optsTemplate
	opts.SSVShare = share
	defer func() { opts.SSVShare = nil }()
	ctx, cancel := context.WithCancel(vm.ctx)
	opts.DutyRunners = SetupRunners(ctx, logger, opts)
	return validator.NewValidator(ctx, cancel, opts)
}

// GetOrCreateValidator creates a new validator instance if not exist
func (vm *validatorsMap) GetOrCreateValidator(logger *zap.Logger, share *types.SSVShare) (*validator.Validator, error) {
	// main lock
	vm.lock.Lock()
	defer vm.lock.Unlock()
	pubKey := hex.EncodeToString(share.ValidatorPubKey)
	if v, ok := vm.validatorsMap[pubKey]; !ok {
		if !share.HasBeaconMetadata() {
			return nil, fmt.Errorf("beacon metadata is missing")
		}
		// Share context with both the validator and the runners,
		// so that when the validator is stopped, the runners are stopped as well.
		vm.validatorsMap[pubKey] = vm.createValidator(logger, share)
		printShare(share, logger, "setup validator done")
	} else {
		printShare(v.Share, logger, "get validator")
	}

	return vm.validatorsMap[pubKey], nil
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
