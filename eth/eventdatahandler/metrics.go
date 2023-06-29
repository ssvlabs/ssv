package eventdatahandler

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
)

// TODO: define
type metrics interface {
	OperatorHasPublicKey(operatorID spectypes.OperatorID, publicKey []byte)
	ValidatorInactive(publicKey []byte)
	ValidatorError(publicKey []byte)
	ValidatorRemoved(publicKey []byte)
}

// nopMetrics is no-op metrics.
type nopMetrics struct{}

func (n nopMetrics) OperatorHasPublicKey(spectypes.OperatorID, []byte) {}
func (n nopMetrics) ValidatorInactive([]byte)                          {}
func (n nopMetrics) ValidatorError([]byte)                             {}
func (n nopMetrics) ValidatorRemoved([]byte)                           {}
