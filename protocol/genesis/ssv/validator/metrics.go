package validator

import (
	"time"

	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"

	"github.com/ssvlabs/ssv/protocol/genesis/ssv/genesisqueue"
)

type Metrics interface {
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

	genesisqueue.Metrics
}

type NopMetrics struct{}

func (n NopMetrics) ValidatorInactive([]byte)                                     {}
func (n NopMetrics) ValidatorNoIndex([]byte)                                      {}
func (n NopMetrics) ValidatorError([]byte)                                        {}
func (n NopMetrics) ValidatorReady([]byte)                                        {}
func (n NopMetrics) ValidatorNotActivated([]byte)                                 {}
func (n NopMetrics) ValidatorExiting([]byte)                                      {}
func (n NopMetrics) ValidatorSlashed([]byte)                                      {}
func (n NopMetrics) ValidatorNotFound([]byte)                                     {}
func (n NopMetrics) ValidatorPending([]byte)                                      {}
func (n NopMetrics) ValidatorRemoved([]byte)                                      {}
func (n NopMetrics) ValidatorUnknown([]byte)                                      {}
func (n NopMetrics) IncomingQueueMessage(genesisspectypes.MessageID)              {}
func (n NopMetrics) OutgoingQueueMessage(genesisspectypes.MessageID)              {}
func (n NopMetrics) DroppedQueueMessage(genesisspectypes.MessageID)               {}
func (n NopMetrics) MessageQueueSize(int)                                         {}
func (n NopMetrics) MessageQueueCapacity(int)                                     {}
func (n NopMetrics) MessageTimeInQueue(genesisspectypes.MessageID, time.Duration) {}
