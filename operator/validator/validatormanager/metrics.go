package validatormanager

import (
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

func (vm *ValidatorManager) reportValidatorStatus(pk []byte, meta *beacon.ValidatorMetadata) {
	logger := vm.logger.With(fields.PubKey(pk), fields.ValidatorMetadata(meta))
	if meta == nil {
		logger.Debug("validator metadata not found")
		vm.metrics.ValidatorNotFound(pk)
	} else if meta.IsActive() {
		logger.Debug("validator is ready")
		vm.metrics.ValidatorReady(pk)
	} else if meta.Slashed() {
		logger.Debug("validator slashed")
		vm.metrics.ValidatorSlashed(pk)
	} else if meta.Exiting() {
		logger.Debug("validator exiting / exited")
		vm.metrics.ValidatorExiting(pk)
	} else if !meta.Activated() {
		logger.Debug("validator not activated")
		vm.metrics.ValidatorNotActivated(pk)
	} else if meta.Pending() {
		logger.Debug("validator pending")
		vm.metrics.ValidatorPending(pk)
	} else if meta.Index == 0 {
		logger.Debug("validator index not found")
		vm.metrics.ValidatorNoIndex(pk)
	} else {
		logger.Debug("validator is unknown")
		vm.metrics.ValidatorUnknown(pk)
	}
}
