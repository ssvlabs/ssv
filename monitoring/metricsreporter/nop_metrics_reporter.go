package metricsreporter

import (
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/libp2p/go-libp2p/core/peer"
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
)

type nopMetrics struct{}

func NewNop() MetricsReporter {
	return &nopMetrics{}
}

func (n *nopMetrics) SSVNodeHealthy()                                                     {}
func (n *nopMetrics) SSVNodeNotHealthy()                                                  {}
func (n *nopMetrics) ExecutionClientReady()                                               {}
func (n *nopMetrics) ExecutionClientSyncing()                                             {}
func (n *nopMetrics) ExecutionClientFailure()                                             {}
func (n *nopMetrics) ExecutionClientLastFetchedBlock(block uint64)                        {}
func (n *nopMetrics) OperatorPublicKey(operatorID spectypes.OperatorID, publicKey []byte) {}
func (n *nopMetrics) ValidatorInactive(publicKey []byte)                                  {}
func (n *nopMetrics) ValidatorNoIndex(publicKey []byte)                                   {}
func (n *nopMetrics) ValidatorError(publicKey []byte)                                     {}
func (n *nopMetrics) ValidatorReady(publicKey []byte)                                     {}
func (n *nopMetrics) ValidatorNotActivated(publicKey []byte)                              {}
func (n *nopMetrics) ValidatorExiting(publicKey []byte)                                   {}
func (n *nopMetrics) ValidatorSlashed(publicKey []byte)                                   {}
func (n *nopMetrics) ValidatorNotFound(publicKey []byte)                                  {}
func (n *nopMetrics) ValidatorPending(publicKey []byte)                                   {}
func (n *nopMetrics) ValidatorRemoved(publicKey []byte)                                   {}
func (n *nopMetrics) ValidatorUnknown(publicKey []byte)                                   {}
func (n *nopMetrics) EventProcessed(eventName string)                                     {}
func (n *nopMetrics) EventProcessingFailed(eventName string)                              {}
func (n *nopMetrics) MessagesReceivedFromPeer(peerId peer.ID)                             {}
func (n *nopMetrics) MessagesReceivedTotal()                                              {}
func (n *nopMetrics) MessageValidationRSAVerifications()                                  {}
func (n *nopMetrics) LastBlockProcessed(block uint64)                                     {}
func (n *nopMetrics) LogsProcessingError(err error)                                       {}
func (n *nopMetrics) GenesisMessageAccepted(role genesisspectypes.BeaconRole, round genesisspecqbft.Round) {
}
func (n *nopMetrics) GenesisMessageIgnored(reason string, role genesisspectypes.BeaconRole, round genesisspecqbft.Round) {
}
func (n *nopMetrics) GenesisMessageRejected(reason string, role genesisspectypes.BeaconRole, round genesisspecqbft.Round) {
}
func (n *nopMetrics) MessageAccepted(role spectypes.RunnerRole, round specqbft.Round)               {}
func (n *nopMetrics) MessageIgnored(reason string, role spectypes.RunnerRole, round specqbft.Round) {}
func (n *nopMetrics) MessageRejected(reason string, role spectypes.RunnerRole, round specqbft.Round) {
}
func (n *nopMetrics) SSVMessageType(msgType spectypes.MsgType)                             {}
func (n *nopMetrics) ConsensusMsgType(msgType specqbft.MessageType, signers int)           {}
func (n *nopMetrics) MessageValidationDuration(duration time.Duration, labels ...string)   {}
func (n *nopMetrics) SignatureValidationDuration(duration time.Duration, labels ...string) {}
func (n *nopMetrics) MessageSize(size int)                                                 {}
func (n *nopMetrics) ActiveMsgValidation(topic string)                                     {}
func (n *nopMetrics) ActiveMsgValidationDone(topic string)                                 {}
func (n *nopMetrics) IncomingQueueMessage(messageID spectypes.MessageID)                   {}
func (n *nopMetrics) OutgoingQueueMessage(messageID spectypes.MessageID)                   {}
func (n *nopMetrics) DroppedQueueMessage(messageID spectypes.MessageID)                    {}
func (n *nopMetrics) MessageQueueSize(size int)                                            {}
func (n *nopMetrics) MessageQueueCapacity(size int)                                        {}
func (n *nopMetrics) MessageTimeInQueue(messageID spectypes.MessageID, d time.Duration)    {}
func (n *nopMetrics) InCommitteeMessage(msgType spectypes.MsgType, decided bool)           {}
func (n *nopMetrics) NonCommitteeMessage(msgType spectypes.MsgType, decided bool)          {}
func (n *nopMetrics) PeerScore(peerId peer.ID, score float64)                              {}
func (n *nopMetrics) PeerP4Score(peerId peer.ID, score float64)                            {}
func (n *nopMetrics) ResetPeerScores()                                                     {}
func (n *nopMetrics) PeerDisconnected(peerId peer.ID)                                      {}
