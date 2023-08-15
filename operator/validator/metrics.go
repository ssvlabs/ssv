package validator

import (
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

func (c *controller) reportValidatorStatus(pk []byte, meta *beacon.ValidatorMetadata) {
	logger := c.logger.With(fields.PubKey(pk), fields.ValidatorMetadata(meta))
	if meta == nil {
		logger.Debug("validator metadata not found")
		c.metrics.ValidatorNotFound(pk)
	} else if meta.IsActive() {
		logger.Debug("validator is ready")
		c.metrics.ValidatorReady(pk)
	} else if meta.Slashed() {
		logger.Debug("validator slashed")
		c.metrics.ValidatorSlashed(pk)
	} else if meta.Exiting() {
		logger.Debug("validator exiting / exited")
		c.metrics.ValidatorExiting(pk)
	} else if !meta.Activated() {
		logger.Debug("validator not activated")
		c.metrics.ValidatorNotActivated(pk)
	} else if meta.Pending() {
		logger.Debug("validator pending")
		c.metrics.ValidatorPending(pk)
	} else if meta.Index == 0 {
		logger.Debug("validator index not found")
		c.metrics.ValidatorNoIndex(pk)
	} else {
		logger.Debug("validator is unknown")
		c.metrics.ValidatorUnknown(pk)
	}
}

type validatorMetrics interface {
	ValidatorInactive(publicKey []byte)
	ValidatorNoIndex(publicKey []byte)
	ValidatorError(publicKey []byte)
	ValidatorReady(publicKey []byte)
	ValidatorNotActivated(publicKey []byte)
	ValidatorExiting(publicKey []byte)
	ValidatorSlashed(publicKey []byte)
	ValidatorNotFound(publicKey []byte)
	ValidatorPending(publicKey []byte)
	ValidatorRemoved(publicKey []byte)
	ValidatorUnknown(publicKey []byte)
}

type nopMetrics struct{}

func (n nopMetrics) ValidatorInactive([]byte)     {}
func (n nopMetrics) ValidatorNoIndex([]byte)      {}
func (n nopMetrics) ValidatorError([]byte)        {}
func (n nopMetrics) ValidatorReady([]byte)        {}
func (n nopMetrics) ValidatorNotActivated([]byte) {}
func (n nopMetrics) ValidatorExiting([]byte)      {}
func (n nopMetrics) ValidatorSlashed([]byte)      {}
func (n nopMetrics) ValidatorNotFound([]byte)     {}
func (n nopMetrics) ValidatorPending([]byte)      {}
func (n nopMetrics) ValidatorRemoved([]byte)      {}
func (n nopMetrics) ValidatorUnknown([]byte)      {}
