package eventhandler

import (
	spectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
)

type metrics interface {
	OperatorPublicKey(operatorID spectypes.OperatorID, publicKey []byte)
	ValidatorInactive(publicKey []byte)
	ValidatorError(publicKey []byte)
	ValidatorRemoved(publicKey []byte)
	EventProcessed(eventName string)
	EventProcessingFailed(eventName string)
}

// nopMetrics is no-op metrics.
type nopMetrics struct{}

func (n nopMetrics) OperatorPublicKey(spectypes.OperatorID, []byte) {}
func (n nopMetrics) ValidatorInactive([]byte)                       {}
func (n nopMetrics) ValidatorError([]byte)                          {}
func (n nopMetrics) ValidatorRemoved([]byte)                        {}
func (n nopMetrics) EventProcessed(string)                          {}
func (n nopMetrics) EventProcessingFailed(string)                   {}
