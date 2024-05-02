package validator

import (
	"time"

	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
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

	queue.Metrics
}

type NopMetrics struct{}

func (n NopMetrics) ValidatorInactive([]byte)                              {}
func (n NopMetrics) ValidatorNoIndex([]byte)                               {}
func (n NopMetrics) ValidatorError([]byte)                                 {}
func (n NopMetrics) ValidatorReady([]byte)                                 {}
func (n NopMetrics) ValidatorNotActivated([]byte)                          {}
func (n NopMetrics) ValidatorExiting([]byte)                               {}
func (n NopMetrics) ValidatorSlashed([]byte)                               {}
func (n NopMetrics) ValidatorNotFound([]byte)                              {}
func (n NopMetrics) ValidatorPending([]byte)                               {}
func (n NopMetrics) ValidatorRemoved([]byte)                               {}
func (n NopMetrics) ValidatorUnknown([]byte)                               {}
func (n NopMetrics) IncomingQueueMessage(spectypes.MessageID)              {}
func (n NopMetrics) OutgoingQueueMessage(spectypes.MessageID)              {}
func (n NopMetrics) DroppedQueueMessage(spectypes.MessageID)               {}
func (n NopMetrics) MessageQueueSize(int)                                  {}
func (n NopMetrics) MessageQueueCapacity(int)                              {}
func (n NopMetrics) MessageTimeInQueue(spectypes.MessageID, time.Duration) {}
