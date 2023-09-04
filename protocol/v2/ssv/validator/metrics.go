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

type nopMetrics struct{}

func (n nopMetrics) ValidatorInactive([]byte)                              {}
func (n nopMetrics) ValidatorNoIndex([]byte)                               {}
func (n nopMetrics) ValidatorError([]byte)                                 {}
func (n nopMetrics) ValidatorReady([]byte)                                 {}
func (n nopMetrics) ValidatorNotActivated([]byte)                          {}
func (n nopMetrics) ValidatorExiting([]byte)                               {}
func (n nopMetrics) ValidatorSlashed([]byte)                               {}
func (n nopMetrics) ValidatorNotFound([]byte)                              {}
func (n nopMetrics) ValidatorPending([]byte)                               {}
func (n nopMetrics) ValidatorRemoved([]byte)                               {}
func (n nopMetrics) ValidatorUnknown([]byte)                               {}
func (n nopMetrics) IncomingQueueMessage(spectypes.MessageID)              {}
func (n nopMetrics) OutgoingQueueMessage(spectypes.MessageID)              {}
func (n nopMetrics) DroppedQueueMessage(spectypes.MessageID)               {}
func (n nopMetrics) MessageQueueSize(int)                                  {}
func (n nopMetrics) MessageQueueCapacity(int)                              {}
func (n nopMetrics) MessageTimeInQueue(spectypes.MessageID, time.Duration) {}
