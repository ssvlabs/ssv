package metricsreporter

import (
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

type nopMetrics struct{}

func NewNop() MetricsReporter {
	return &nopMetrics{}
}

func (n *nopMetrics) SSVNodeHealthy()                                                     {}
func (n *nopMetrics) SSVNodeNotHealthy()                                                  {}
func (n *nopMetrics) OperatorPublicKey(operatorID spectypes.OperatorID, publicKey []byte) {}
func (n *nopMetrics) MessageValidationRSAVerifications()                                  {}
func (n *nopMetrics) GenesisMessageAccepted(role genesisspectypes.BeaconRole, round genesisspecqbft.Round) {
}
func (n *nopMetrics) GenesisMessageIgnored(reason string, role genesisspectypes.BeaconRole, round genesisspecqbft.Round) {
}
func (n *nopMetrics) GenesisMessageRejected(reason string, role genesisspectypes.BeaconRole, round genesisspecqbft.Round) {
}
func (n *nopMetrics) SignatureValidationDuration(duration time.Duration, labels ...string) {}
func (n *nopMetrics) IncomingQueueMessage(messageID spectypes.MessageID)                   {}
func (n *nopMetrics) OutgoingQueueMessage(messageID spectypes.MessageID)                   {}
func (n *nopMetrics) MessageQueueSize(size int)                                            {}
func (n *nopMetrics) MessageQueueCapacity(size int)                                        {}
func (n *nopMetrics) MessageTimeInQueue(messageID spectypes.MessageID, d time.Duration)    {}
func (n *nopMetrics) InCommitteeMessage(msgType spectypes.MsgType, decided bool)           {}
func (n *nopMetrics) NonCommitteeMessage(msgType spectypes.MsgType, decided bool)          {}
func (n *nopMetrics) PeerScore(peerId peer.ID, score float64)                              {}
func (n *nopMetrics) PeerP4Score(peerId peer.ID, score float64)                            {}
func (n *nopMetrics) ResetPeerScores()                                                     {}
func (n *nopMetrics) PeerDisconnected(peerId peer.ID)                                      {}
