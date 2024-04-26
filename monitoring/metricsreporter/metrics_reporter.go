package metricsreporter

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v2/message"
	ssvmessage "github.com/bloxapp/ssv/protocol/v2/message"
)

const (
	ssvNodeNotHealthy = float64(0)
	ssvNodeHealthy    = float64(1)

	executionClientFailure = float64(0)
	executionClientSyncing = float64(1)
	executionClientOK      = float64(2)

	validatorInactive     = float64(0)
	validatorNoIndex      = float64(1)
	validatorError        = float64(2)
	validatorReady        = float64(3)
	validatorNotActivated = float64(4)
	validatorExiting      = float64(5)
	validatorSlashed      = float64(6)
	validatorNotFound     = float64(7)
	validatorPending      = float64(8)
	validatorRemoved      = float64(9)
	validatorUnknown      = float64(10)

	messageAccepted = "accepted"
	messageIgnored  = "ignored"
	messageRejected = "rejected"
)

var (
	ssvNodeStatus = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv_node_status",
		Help: "Status of the operator node",
	})
	// TODO: rename "eth1" in metrics
	executionClientStatus = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv_eth1_status",
		Help: "Status of the connected execution client",
	})
	executionClientLastFetchedBlock = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv_execution_client_last_fetched_block",
		Help: "Last fetched block by execution client",
	})
	validatorStatus = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:v2:status",
		Help: "Validator status",
	}, []string{"pubKey"})
	eventProcessed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_eth1_sync_count_success",
		Help: "Count succeeded execution client events",
	}, []string{"etype"})
	eventProcessingFailed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_eth1_sync_count_failed",
		Help: "Count failed execution client events",
	}, []string{"etype"})
	operatorIndex = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:exporter:operator_index",
		Help: "operator footprint",
	}, []string{"pubKey", "index"})
	messageValidationResult = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_validation",
		Help: "Message validation result",
	}, []string{"status", "reason", "role", "round"})
	messageValidationSSVType = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_validation_ssv_type",
		Help: "SSV message type",
	}, []string{"type"})
	messageValidationConsensusType = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_validation_consensus_type",
		Help: "Consensus message type",
	}, []string{"type", "signers"})
	messageValidationDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_message_validation_duration_seconds",
		Help:    "Message validation duration (seconds)",
		Buckets: []float64{0.001, 0.005, 0.010, 0.020, 0.050},
	}, []string{})
	signatureValidationDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_signature_validation_duration_seconds",
		Help:    "Signature validation duration (seconds)",
		Buckets: []float64{0.001, 0.005, 0.010, 0.020, 0.050},
	}, []string{})
	messageSize = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_message_size",
		Help:    "Message size",
		Buckets: []float64{100, 500, 1_000, 5_000, 10_000, 50_000, 100_000, 500_000, 1_000_000, 5_000_000},
	}, []string{})
	activeMsgValidation = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:p2p:pubsub:msg:val:active",
		Help: "Count active message validation",
	}, []string{"topic"})
	incomingQueueMessages = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_queue_incoming",
		Help: "The amount of message incoming to the validator's msg queue",
	}, []string{"msg_id"})
	outgoingQueueMessages = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_queue_outgoing",
		Help: "The amount of message outgoing from the validator's msg queue",
	}, []string{"msg_id"})
	droppedQueueMessages = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_queue_drops",
		Help: "The amount of message dropped from the validator's msg queue",
	}, []string{"msg_id"})
	messageQueueSize = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv_message_queue_size",
		Help: "Size of message queue",
	}, []string{})
	messageQueueCapacity = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv_message_queue_capacity",
		Help: "Capacity of message queue",
	}, []string{})
	messageTimeInQueue = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_message_time_in_queue_seconds",
		Help:    "Time message spent in queue (seconds)",
		Buckets: []float64{0.001, 0.005, 0.010, 0.050, 0.100, 0.500, 1, 5, 10, 60},
	}, []string{"msg_id"})
	inCommitteeMessages = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_in_committee",
		Help: "The amount of messages in committee",
	}, []string{"ssv_msg_type", "decided"})
	nonCommitteeMessages = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_non_committee",
		Help: "The amount of messages not in committee",
	}, []string{"ssv_msg_type", "decided"})
	messagesReceivedFromPeer = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_messages_received_from_peer",
		Help: "The amount of messages received from the specific peer",
	}, []string{"peer_id"})
	messagesReceivedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_messages_received_total",
		Help: "The amount of messages total received",
	}, []string{})
	messageValidationRSAVerifications = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_validation_rsa_checks",
		Help: "The amount message validations",
	}, []string{})
	pubsubPeerScore = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:p2p:pubsub:score:inspect",
		Help: "Pubsub peer scores",
	}, []string{"pid"})
	pubsubPeerP4Score = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:p2p:pubsub:score:invalid_message_deliveries",
		Help: "Pubsub peer P4 scores (sum of square of counters for invalid message deliveries)",
	}, []string{"pid"})
)

type MetricsReporter interface {
	Genesis // DEPRECATED

	SSVNodeHealthy()
	SSVNodeNotHealthy()
	ExecutionClientReady()
	ExecutionClientSyncing()
	ExecutionClientFailure()
	ExecutionClientLastFetchedBlock(block uint64)
	OperatorPublicKey(operatorID spectypes.OperatorID, publicKey []byte)
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
	EventProcessed(eventName string)
	EventProcessingFailed(eventName string)
	MessagesReceivedFromPeer(peerId peer.ID)
	MessagesReceivedTotal()
	MessageValidationRSAVerifications()
	LastBlockProcessed(block uint64)
	LogsProcessingError(err error)
	MessageAccepted(role spectypes.RunnerRole, round specqbft.Round)
	MessageIgnored(reason string, role spectypes.RunnerRole, round specqbft.Round)
	MessageRejected(reason string, role spectypes.RunnerRole, round specqbft.Round)
	SSVMessageType(msgType spectypes.MsgType)
	ConsensusMsgType(msgType specqbft.MessageType, signers int)
	MessageValidationDuration(duration time.Duration, labels ...string)
	SignatureValidationDuration(duration time.Duration, labels ...string)
	MessageSize(size int)
	ActiveMsgValidation(topic string)
	ActiveMsgValidationDone(topic string)
	IncomingQueueMessage(messageID spectypes.MessageID)
	OutgoingQueueMessage(messageID spectypes.MessageID)
	DroppedQueueMessage(messageID spectypes.MessageID)
	MessageQueueSize(size int)
	MessageQueueCapacity(size int)
	MessageTimeInQueue(messageID spectypes.MessageID, d time.Duration)
	CommitteeMessage(msgType spectypes.MsgType, decided bool)
	NonCommitteeMessage(msgType spectypes.MsgType, decided bool)
	PeerScore(peerId peer.ID, score float64)
	PeerP4Score(peerId peer.ID, score float64)
	ResetPeerScores()
	PeerDisconnected(peerId peer.ID)
}

type metricsReporter struct {
	logger *zap.Logger
}

func New(opts ...Option) MetricsReporter {
	mr := &metricsReporter{
		logger: zap.NewNop(),
	}

	for _, opt := range opts {
		opt(mr)
	}

	// TODO: think how to register all metrics without adding them all to the slice
	allMetrics := []prometheus.Collector{
		ssvNodeStatus,
		executionClientStatus,
		executionClientLastFetchedBlock,
		validatorStatus,
		eventProcessed,
		eventProcessingFailed,
		operatorIndex,
		messageValidationResult,
		messageValidationSSVType,
		messageValidationConsensusType,
		messageValidationDuration,
		signatureValidationDuration,
		messageSize,
		activeMsgValidation,
		incomingQueueMessages,
		outgoingQueueMessages,
		droppedQueueMessages,
		messageQueueSize,
		messageQueueCapacity,
		messageTimeInQueue,
		inCommitteeMessages,
		nonCommitteeMessages,
		messagesReceivedFromPeer,
		messagesReceivedTotal,
		messageValidationRSAVerifications,
		pubsubPeerScore,
		pubsubPeerP4Score,
	}

	for i, c := range allMetrics {
		if err := prometheus.Register(c); err != nil {
			// TODO: think how to print metric name
			mr.logger.Debug("could not register prometheus collector",
				zap.Int("index", i),
				zap.Error(err),
			)
		}
	}

	return &metricsReporter{}
}

func (m *metricsReporter) SSVNodeHealthy() {
	ssvNodeStatus.Set(ssvNodeHealthy)
}

func (m *metricsReporter) SSVNodeNotHealthy() {
	ssvNodeStatus.Set(ssvNodeNotHealthy)
}

func (m *metricsReporter) ExecutionClientReady() {
	executionClientStatus.Set(executionClientOK)
}

func (m *metricsReporter) ExecutionClientSyncing() {
	executionClientStatus.Set(executionClientSyncing)
}

func (m *metricsReporter) ExecutionClientFailure() {
	executionClientStatus.Set(executionClientFailure)
}

func (m *metricsReporter) ExecutionClientLastFetchedBlock(block uint64) {
	executionClientLastFetchedBlock.Set(float64(block))
}

func (m *metricsReporter) OperatorPublicKey(operatorID spectypes.OperatorID, publicKey []byte) {
	pkHash := fmt.Sprintf("%x", sha256.Sum256(publicKey))
	operatorIndex.WithLabelValues(pkHash, strconv.FormatUint(operatorID, 10)).Set(float64(operatorID))
}

func (m *metricsReporter) ValidatorInactive(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorInactive)
}
func (m *metricsReporter) ValidatorNoIndex(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorNoIndex)
}
func (m *metricsReporter) ValidatorError(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorError)
}
func (m *metricsReporter) ValidatorReady(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorReady)
}
func (m *metricsReporter) ValidatorNotActivated(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorNotActivated)
}
func (m *metricsReporter) ValidatorExiting(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorExiting)
}
func (m *metricsReporter) ValidatorSlashed(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorSlashed)
}
func (m *metricsReporter) ValidatorNotFound(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorNotFound)
}
func (m *metricsReporter) ValidatorPending(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorPending)
}
func (m *metricsReporter) ValidatorRemoved(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorRemoved)
}
func (m *metricsReporter) ValidatorUnknown(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorUnknown)
}

func (m *metricsReporter) EventProcessed(eventName string) {
	eventProcessed.WithLabelValues(eventName).Inc()
}

func (m *metricsReporter) EventProcessingFailed(eventName string) {
	eventProcessingFailed.WithLabelValues(eventName).Inc()
}

func (m *metricsReporter) MessagesReceivedFromPeer(peerId peer.ID) {
	messagesReceivedFromPeer.WithLabelValues(peerId.String()).Inc()
}

func (m *metricsReporter) MessagesReceivedTotal() {
	messagesReceivedTotal.WithLabelValues().Inc()
}

func (m *metricsReporter) MessageValidationRSAVerifications() {
	messageValidationRSAVerifications.WithLabelValues().Inc()
}

// TODO implement
func (m *metricsReporter) LastBlockProcessed(uint64) {}
func (m *metricsReporter) LogsProcessingError(error) {}

func (m *metricsReporter) MessageAccepted(
	role spectypes.RunnerRole,
	round specqbft.Round,
) {
	messageValidationResult.WithLabelValues(
		messageAccepted,
		"",
		message.RunnerRoleToString(role),
		strconv.FormatUint(uint64(round), 10),
	).Inc()
}

func (m *metricsReporter) MessageIgnored(
	reason string,
	role spectypes.RunnerRole,
	round specqbft.Round,
) {
	messageValidationResult.WithLabelValues(
		messageIgnored,
		reason,
		message.RunnerRoleToString(role),
		strconv.FormatUint(uint64(round), 10),
	).Inc()
}

func (m *metricsReporter) MessageRejected(
	reason string,
	role spectypes.RunnerRole,
	round specqbft.Round,
) {
	messageValidationResult.WithLabelValues(
		messageRejected,
		reason,
		message.RunnerRoleToString(role),
		strconv.FormatUint(uint64(round), 10),
	).Inc()
}

func (m *metricsReporter) SSVMessageType(msgType spectypes.MsgType) {
	messageValidationSSVType.WithLabelValues(ssvmessage.MsgTypeToString(msgType)).Inc()
}

func (m *metricsReporter) ConsensusMsgType(msgType specqbft.MessageType, signers int) {
	messageValidationConsensusType.WithLabelValues(ssvmessage.QBFTMsgTypeToString(msgType), strconv.Itoa(signers)).Inc()
}

func (m *metricsReporter) MessageValidationDuration(duration time.Duration, labels ...string) {
	messageValidationDuration.WithLabelValues(labels...).Observe(duration.Seconds())
}

func (m *metricsReporter) SignatureValidationDuration(duration time.Duration, labels ...string) {
	signatureValidationDuration.WithLabelValues(labels...).Observe(duration.Seconds())
}

func (m *metricsReporter) MessageSize(size int) {
	messageSize.WithLabelValues().Observe(float64(size))
}

func (m *metricsReporter) ActiveMsgValidation(topic string) {
	activeMsgValidation.WithLabelValues(topic).Inc()
}

func (m *metricsReporter) ActiveMsgValidationDone(topic string) {
	activeMsgValidation.WithLabelValues(topic).Dec()
}

func (m *metricsReporter) IncomingQueueMessage(messageID spectypes.MessageID) {
	incomingQueueMessages.WithLabelValues(messageID.String()).Inc()
}

func (m *metricsReporter) OutgoingQueueMessage(messageID spectypes.MessageID) {
	outgoingQueueMessages.WithLabelValues(messageID.String()).Inc()
}

func (m *metricsReporter) DroppedQueueMessage(messageID spectypes.MessageID) {
	droppedQueueMessages.WithLabelValues(messageID.String()).Inc()
}

func (m *metricsReporter) MessageQueueSize(size int) {
	messageQueueSize.WithLabelValues().Set(float64(size))
}

func (m *metricsReporter) MessageQueueCapacity(size int) {
	messageQueueCapacity.WithLabelValues().Set(float64(size))
}

func (m *metricsReporter) MessageTimeInQueue(messageID spectypes.MessageID, d time.Duration) {
	messageTimeInQueue.WithLabelValues(messageID.String()).Observe(d.Seconds())
}

func (m *metricsReporter) CommitteeMessage(msgType spectypes.MsgType, decided bool) {
	str := "non-decided"
	if decided {
		str = "decided"
	}
	inCommitteeMessages.WithLabelValues(ssvmessage.MsgTypeToString(msgType), str).Inc()
}

func (m *metricsReporter) NonCommitteeMessage(msgType spectypes.MsgType, decided bool) {
	str := "non-decided"
	if decided {
		str = "decided"
	}
	nonCommitteeMessages.WithLabelValues(ssvmessage.MsgTypeToString(msgType), str).Inc()
}

func (m *metricsReporter) PeerScore(peerId peer.ID, score float64) {
	pubsubPeerScore.WithLabelValues(peerId.String()).Set(score)
}

func (m *metricsReporter) PeerP4Score(peerId peer.ID, score float64) {
	pubsubPeerP4Score.WithLabelValues(peerId.String()).Set(score)
}

func (m *metricsReporter) ResetPeerScores() {
	pubsubPeerScore.Reset()
	pubsubPeerP4Score.Reset()
}

// PeerDisconnected deletes all data about peers which connections have been closed by the current node
func (m *metricsReporter) PeerDisconnected(peerId peer.ID) {
	messagesReceivedFromPeer.DeleteLabelValues(peerId.String())
}
