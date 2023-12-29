package metricsreporter

import (
	"net"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	ps_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

type nopMetrics struct{}

func NewNop() MetricsReporter {
	return &nopMetrics{}
}

func (n *nopMetrics) SSVNodeHealthy()                                                           {}
func (n *nopMetrics) SSVNodeNotHealthy()                                                        {}
func (n *nopMetrics) ExecutionClientReady()                                                     {}
func (n *nopMetrics) ExecutionClientSyncing()                                                   {}
func (n *nopMetrics) ExecutionClientFailure()                                                   {}
func (n *nopMetrics) ExecutionClientLastFetchedBlock(block uint64)                              {}
func (n *nopMetrics) ConsensusClientReady()                                                     {}
func (n *nopMetrics) ConsensusClientSyncing()                                                   {}
func (n *nopMetrics) ConsensusClientUnknown()                                                   {}
func (n *nopMetrics) AttesterDataRequest(duration time.Duration)                                {}
func (n *nopMetrics) AggregatorDataRequest(duration time.Duration)                              {}
func (n *nopMetrics) ProposerDataRequest(duration time.Duration)                                {}
func (n *nopMetrics) SyncCommitteeDataRequest(duration time.Duration)                           {}
func (n *nopMetrics) SyncCommitteeContributionDataRequest(duration time.Duration)               {}
func (n *nopMetrics) ConsensusDutySubmission(role spectypes.BeaconRole, duration time.Duration) {}
func (n *nopMetrics) QBFTProposalDuration(duration time.Duration)                               {}
func (n *nopMetrics) QBFTPrepareDuration(duration time.Duration)                                {}
func (n *nopMetrics) QBFTCommitDuration(duration time.Duration)                                 {}
func (n *nopMetrics) QBFTRound(msgID spectypes.MessageID, round specqbft.Round)                 {}
func (n *nopMetrics) ConsensusDuration(role spectypes.BeaconRole, duration time.Duration)       {}
func (n *nopMetrics) PreConsensusDuration(role spectypes.BeaconRole, duration time.Duration)    {}
func (n *nopMetrics) PostConsensusDuration(role spectypes.BeaconRole, duration time.Duration)   {}
func (n *nopMetrics) DutyFullFlowDuration(role spectypes.BeaconRole, duration time.Duration)    {}
func (n *nopMetrics) DutyFullFlowFirstRoundDuration(role spectypes.BeaconRole, duration time.Duration) {
}
func (n *nopMetrics) RoleSubmitted(role spectypes.BeaconRole)                                       {}
func (n *nopMetrics) RoleSubmissionFailure(role spectypes.BeaconRole)                               {}
func (n *nopMetrics) InstanceStarted(role spectypes.BeaconRole)                                     {}
func (n *nopMetrics) InstanceDecided(role spectypes.BeaconRole)                                     {}
func (n *nopMetrics) WorkerProcessedMessage(prefix string)                                          {}
func (n *nopMetrics) OperatorPublicKey(operatorID spectypes.OperatorID, publicKey []byte)           {}
func (n *nopMetrics) ValidatorInactive(publicKey []byte)                                            {}
func (n *nopMetrics) ValidatorNoIndex(publicKey []byte)                                             {}
func (n *nopMetrics) ValidatorError(publicKey []byte)                                               {}
func (n *nopMetrics) ValidatorReady(publicKey []byte)                                               {}
func (n *nopMetrics) ValidatorNotActivated(publicKey []byte)                                        {}
func (n *nopMetrics) ValidatorExiting(publicKey []byte)                                             {}
func (n *nopMetrics) ValidatorSlashed(publicKey []byte)                                             {}
func (n *nopMetrics) ValidatorNotFound(publicKey []byte)                                            {}
func (n *nopMetrics) ValidatorPending(publicKey []byte)                                             {}
func (n *nopMetrics) ValidatorRemoved(publicKey []byte)                                             {}
func (n *nopMetrics) ValidatorUnknown(publicKey []byte)                                             {}
func (n *nopMetrics) EventProcessed(eventName string)                                               {}
func (n *nopMetrics) EventProcessingFailed(eventName string)                                        {}
func (n *nopMetrics) MessagesReceivedFromPeer(peerId peer.ID)                                       {}
func (n *nopMetrics) MessagesReceivedTotal()                                                        {}
func (n *nopMetrics) MessageValidationRSAVerifications()                                            {}
func (n *nopMetrics) LastBlockProcessed(block uint64)                                               {}
func (n *nopMetrics) LogsProcessingError(err error)                                                 {}
func (n *nopMetrics) MessageAccepted(role spectypes.BeaconRole, round specqbft.Round)               {}
func (n *nopMetrics) MessageIgnored(reason string, role spectypes.BeaconRole, round specqbft.Round) {}
func (n *nopMetrics) MessageRejected(reason string, role spectypes.BeaconRole, round specqbft.Round) {
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
func (n *nopMetrics) AddNetworkConnection()                                                {}
func (n *nopMetrics) RemoveNetworkConnection()                                             {}
func (n *nopMetrics) FilteredNetworkConnection()                                           {}
func (n *nopMetrics) PubsubTrace(eventType ps_pb.TraceEvent_Type)                          {}
func (n *nopMetrics) PubsubOutbound(topicName string)                                      {}
func (n *nopMetrics) PubsubInbound(topicName string, msgType spectypes.MsgType)            {}
func (n *nopMetrics) AllConnectedPeers(count int)                                          {}
func (n *nopMetrics) ConnectedTopicPeers(topic string, count int)                          {}
func (n *nopMetrics) PeersIdentity(opPKHash, opID, nodeVersion, pid, nodeType string)      {}
func (n *nopMetrics) RouterIncoming(msgType spectypes.MsgType)                             {}
func (n *nopMetrics) KnownSubnetPeers(subnet, count int)                                   {}
func (n *nopMetrics) ConnectedSubnetPeers(subnet, count int)                               {}
func (n *nopMetrics) MySubnets(subnet int, existence byte)                                 {}
func (n *nopMetrics) OutgoingStreamRequest(protocol protocol.ID)                           {}
func (n *nopMetrics) AddActiveStreamRequest(protocol protocol.ID)                          {}
func (n *nopMetrics) RemoveActiveStreamRequest(protocol protocol.ID)                       {}
func (n *nopMetrics) SuccessfulStreamRequest(protocol protocol.ID)                         {}
func (n *nopMetrics) StreamResponse(protocol protocol.ID)                                  {}
func (n *nopMetrics) StreamRequest(protocol protocol.ID)                                   {}
func (n *nopMetrics) NodeRejected()                                                        {}
func (n *nopMetrics) NodeFound()                                                           {}
func (n *nopMetrics) ENRPing()                                                             {}
func (n *nopMetrics) ENRPong()                                                             {}
func (n *nopMetrics) SlotDelay(delay time.Duration)                                        {}
func (n *nopMetrics) StreamOutbound(addr net.Addr)                                         {}
func (n *nopMetrics) StreamOutboundError(addr net.Addr)                                    {}
