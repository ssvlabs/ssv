package metricsreporter

import (
	"crypto/sha256"
	"fmt"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"strconv"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

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

type MetricsReporter interface {
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
	MessageAccepted(role spectypes.BeaconRole, round specqbft.Round)
	MessageIgnored(reason string, role spectypes.BeaconRole, round specqbft.Round)
	MessageRejected(reason string, role spectypes.BeaconRole, round specqbft.Round)
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
	InCommitteeMessage(msgType spectypes.MsgType, decided bool)
	NonCommitteeMessage(msgType spectypes.MsgType, decided bool)
	PeerScore(peerId peer.ID, score float64)
	PeerP4Score(peerId peer.ID, score float64)
	ResetPeerScores()
	PeerDisconnected(peerId peer.ID)
}

type metricsReporter struct {
	logger *zap.Logger

	ssvNodeStatus                     prometheus.Gauge
	executionClientStatus             prometheus.Gauge
	executionClientLastFetchedBlock   prometheus.Gauge
	validatorStatus                   *prometheus.GaugeVec
	eventProcessed                    *prometheus.CounterVec
	eventProcessingFailed             *prometheus.CounterVec
	operatorIndex                     *prometheus.GaugeVec
	messageValidationResult           *prometheus.CounterVec
	messageValidationSSVType          *prometheus.CounterVec
	messageValidationConsensusType    *prometheus.CounterVec
	messageValidationDuration         *prometheus.HistogramVec
	signatureValidationDuration       *prometheus.HistogramVec
	messageSize                       *prometheus.HistogramVec
	activeMsgValidation               *prometheus.GaugeVec
	incomingQueueMessages             *prometheus.CounterVec
	outgoingQueueMessages             *prometheus.CounterVec
	droppedQueueMessages              *prometheus.CounterVec
	messageQueueSize                  *prometheus.GaugeVec
	messageQueueCapacity              *prometheus.GaugeVec
	messageTimeInQueue                *prometheus.HistogramVec
	inCommitteeMessages               *prometheus.CounterVec
	nonCommitteeMessages              *prometheus.CounterVec
	messagesReceivedFromPeer          *prometheus.CounterVec
	messagesReceivedTotal             *prometheus.CounterVec
	messageValidationRSAVerifications *prometheus.CounterVec
	pubsubPeerScore                   *prometheus.GaugeVec
	pubsubPeerP4Score                 *prometheus.GaugeVec
}

func New(reg *prometheus.Registry, opts ...Option) MetricsReporter {
	mr := &metricsReporter{
		logger: zap.NewNop(),
	}

	mr.ssvNodeStatus = promauto.With(reg).NewGauge(prometheus.GaugeOpts{
		Name: "ssv_node_status",
		Help: "Status of the operator node",
	})
	// TODO: rename "eth1" in metrics
	mr.executionClientStatus = promauto.With(reg).NewGauge(prometheus.GaugeOpts{
		Name: "ssv_eth1_status",
		Help: "Status of the connected execution client",
	})
	mr.executionClientLastFetchedBlock = promauto.With(reg).NewGauge(prometheus.GaugeOpts{
		Name: "ssv_execution_client_last_fetched_block",
		Help: "Last fetched block by execution client",
	})
	mr.validatorStatus = promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:v2:status",
		Help: "Validator status",
	}, []string{"pubKey"})
	mr.eventProcessed = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_eth1_sync_count_success",
		Help: "Count succeeded execution client events",
	}, []string{"etype"})
	mr.eventProcessingFailed = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_eth1_sync_count_failed",
		Help: "Count failed execution client events",
	}, []string{"etype"})
	mr.operatorIndex = promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:exporter:operator_index",
		Help: "operator footprint",
	}, []string{"pubKey", "index"})
	mr.messageValidationResult = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_validation",
		Help: "Message validation result",
	}, []string{"status", "reason", "role", "round"})
	mr.messageValidationSSVType = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_validation_ssv_type",
		Help: "SSV message type",
	}, []string{"type"})
	mr.messageValidationConsensusType = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_validation_consensus_type",
		Help: "Consensus message type",
	}, []string{"type", "signers"})
	mr.messageValidationDuration = promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_message_validation_duration_seconds",
		Help:    "Message validation duration (seconds)",
		Buckets: []float64{0.001, 0.005, 0.010, 0.020, 0.050},
	}, []string{})
	mr.signatureValidationDuration = promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_signature_validation_duration_seconds",
		Help:    "Signature validation duration (seconds)",
		Buckets: []float64{0.001, 0.005, 0.010, 0.020, 0.050},
	}, []string{})
	mr.messageSize = promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_message_size",
		Help:    "Message size",
		Buckets: []float64{100, 500, 1_000, 5_000, 10_000, 50_000, 100_000, 500_000, 1_000_000, 5_000_000},
	}, []string{})
	mr.activeMsgValidation = promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:p2p:pubsub:msg:val:active",
		Help: "Count active message validation",
	}, []string{"topic"})
	mr.incomingQueueMessages = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_queue_incoming",
		Help: "The amount of message incoming to the validator's msg queue",
	}, []string{"msg_id"})
	mr.outgoingQueueMessages = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_queue_outgoing",
		Help: "The amount of message outgoing from the validator's msg queue",
	}, []string{"msg_id"})
	mr.droppedQueueMessages = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_queue_drops",
		Help: "The amount of message dropped from the validator's msg queue",
	}, []string{"msg_id"})
	mr.messageQueueSize = promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv_message_queue_size",
		Help: "Size of message queue",
	}, []string{})
	mr.messageQueueCapacity = promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv_message_queue_capacity",
		Help: "Capacity of message queue",
	}, []string{})
	mr.messageTimeInQueue = promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_message_time_in_queue_seconds",
		Help:    "Time message spent in queue (seconds)",
		Buckets: []float64{0.001, 0.005, 0.010, 0.050, 0.100, 0.500, 1, 5, 10, 60},
	}, []string{"msg_id"})
	mr.inCommitteeMessages = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_in_committee",
		Help: "The amount of messages in committee",
	}, []string{"ssv_msg_type", "decided"})
	mr.nonCommitteeMessages = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_non_committee",
		Help: "The amount of messages not in committee",
	}, []string{"ssv_msg_type", "decided"})
	mr.messagesReceivedFromPeer = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_messages_received_from_peer",
		Help: "The amount of messages received from the specific peer",
	}, []string{"peer_id"})
	mr.messagesReceivedTotal = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_messages_received_total",
		Help: "The amount of messages total received",
	}, []string{})
	mr.messageValidationRSAVerifications = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_validation_rsa_checks",
		Help: "The amount message validations",
	}, []string{})
	mr.pubsubPeerScore = promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:p2p:pubsub:score:inspect",
		Help: "Pubsub peer scores",
	}, []string{"pid"})
	mr.pubsubPeerP4Score = promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:p2p:pubsub:score:invalid_message_deliveries",
		Help: "Pubsub peer P4 scores (sum of square of counters for invalid message deliveries)",
	}, []string{"pid"})

	for _, opt := range opts {
		opt(mr)
	}

	// TODO: think how to register all metrics without adding them all to the slice
	allMetrics := []prometheus.Collector{
		mr.ssvNodeStatus,
		mr.executionClientStatus,
		mr.executionClientLastFetchedBlock,
		mr.validatorStatus,
		mr.eventProcessed,
		mr.eventProcessingFailed,
		mr.operatorIndex,
		mr.messageValidationResult,
		mr.messageValidationSSVType,
		mr.messageValidationConsensusType,
		mr.messageValidationDuration,
		mr.signatureValidationDuration,
		mr.messageSize,
		mr.activeMsgValidation,
		mr.incomingQueueMessages,
		mr.outgoingQueueMessages,
		mr.droppedQueueMessages,
		mr.messageQueueSize,
		mr.messageQueueCapacity,
		mr.messageTimeInQueue,
		mr.inCommitteeMessages,
		mr.nonCommitteeMessages,
		mr.messagesReceivedFromPeer,
		mr.messagesReceivedTotal,
		mr.messageValidationRSAVerifications,
		mr.pubsubPeerScore,
		mr.pubsubPeerP4Score,
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
	m.ssvNodeStatus.Set(ssvNodeHealthy)
}

func (m *metricsReporter) SSVNodeNotHealthy() {
	m.ssvNodeStatus.Set(ssvNodeNotHealthy)
}

func (m *metricsReporter) ExecutionClientReady() {
	m.executionClientStatus.Set(executionClientOK)
}

func (m *metricsReporter) ExecutionClientSyncing() {
	m.executionClientStatus.Set(executionClientSyncing)
}

func (m *metricsReporter) ExecutionClientFailure() {
	m.executionClientStatus.Set(executionClientFailure)
}

func (m *metricsReporter) ExecutionClientLastFetchedBlock(block uint64) {
	m.executionClientLastFetchedBlock.Set(float64(block))
}

func (m *metricsReporter) OperatorPublicKey(operatorID spectypes.OperatorID, publicKey []byte) {
	pkHash := fmt.Sprintf("%x", sha256.Sum256(publicKey))
	m.operatorIndex.WithLabelValues(pkHash, strconv.FormatUint(operatorID, 10)).Set(float64(operatorID))
}

func (m *metricsReporter) ValidatorInactive(publicKey []byte) {
	m.validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorInactive)
}
func (m *metricsReporter) ValidatorNoIndex(publicKey []byte) {
	m.validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorNoIndex)
}
func (m *metricsReporter) ValidatorError(publicKey []byte) {
	m.validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorError)
}
func (m *metricsReporter) ValidatorReady(publicKey []byte) {
	m.validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorReady)
}
func (m *metricsReporter) ValidatorNotActivated(publicKey []byte) {
	m.validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorNotActivated)
}
func (m *metricsReporter) ValidatorExiting(publicKey []byte) {
	m.validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorExiting)
}
func (m *metricsReporter) ValidatorSlashed(publicKey []byte) {
	m.validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorSlashed)
}
func (m *metricsReporter) ValidatorNotFound(publicKey []byte) {
	m.validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorNotFound)
}
func (m *metricsReporter) ValidatorPending(publicKey []byte) {
	m.validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorPending)
}
func (m *metricsReporter) ValidatorRemoved(publicKey []byte) {
	m.validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorRemoved)
}
func (m *metricsReporter) ValidatorUnknown(publicKey []byte) {
	m.validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorUnknown)
}

func (m *metricsReporter) EventProcessed(eventName string) {
	m.eventProcessed.WithLabelValues(eventName).Inc()
}

func (m *metricsReporter) EventProcessingFailed(eventName string) {
	m.eventProcessingFailed.WithLabelValues(eventName).Inc()
}

func (m *metricsReporter) MessagesReceivedFromPeer(peerId peer.ID) {
	m.messagesReceivedFromPeer.WithLabelValues(peerId.String()).Inc()
}

func (m *metricsReporter) MessagesReceivedTotal() {
	m.messagesReceivedTotal.WithLabelValues().Inc()
}

func (m *metricsReporter) MessageValidationRSAVerifications() {
	m.messageValidationRSAVerifications.WithLabelValues().Inc()
}

// TODO implement
func (m *metricsReporter) LastBlockProcessed(uint64) {}
func (m *metricsReporter) LogsProcessingError(error) {}

func (m *metricsReporter) MessageAccepted(
	role spectypes.BeaconRole,
	round specqbft.Round,
) {
	m.messageValidationResult.WithLabelValues(
		messageAccepted,
		"",
		role.String(),
		strconv.FormatUint(uint64(round), 10),
	).Inc()
}

func (m *metricsReporter) MessageIgnored(
	reason string,
	role spectypes.BeaconRole,
	round specqbft.Round,
) {
	m.messageValidationResult.WithLabelValues(
		messageIgnored,
		reason,
		role.String(),
		strconv.FormatUint(uint64(round), 10),
	).Inc()
}

func (m *metricsReporter) MessageRejected(
	reason string,
	role spectypes.BeaconRole,
	round specqbft.Round,
) {
	m.messageValidationResult.WithLabelValues(
		messageRejected,
		reason,
		role.String(),
		strconv.FormatUint(uint64(round), 10),
	).Inc()
}

func (m *metricsReporter) SSVMessageType(msgType spectypes.MsgType) {
	m.messageValidationSSVType.WithLabelValues(ssvmessage.MsgTypeToString(msgType)).Inc()
}

func (m *metricsReporter) ConsensusMsgType(msgType specqbft.MessageType, signers int) {
	m.messageValidationConsensusType.WithLabelValues(ssvmessage.QBFTMsgTypeToString(msgType), strconv.Itoa(signers)).Inc()
}

func (m *metricsReporter) MessageValidationDuration(duration time.Duration, labels ...string) {
	m.messageValidationDuration.WithLabelValues(labels...).Observe(duration.Seconds())
}

func (m *metricsReporter) SignatureValidationDuration(duration time.Duration, labels ...string) {
	m.signatureValidationDuration.WithLabelValues(labels...).Observe(duration.Seconds())
}

func (m *metricsReporter) MessageSize(size int) {
	m.messageSize.WithLabelValues().Observe(float64(size))
}

func (m *metricsReporter) ActiveMsgValidation(topic string) {
	m.activeMsgValidation.WithLabelValues(topic).Inc()
}

func (m *metricsReporter) ActiveMsgValidationDone(topic string) {
	m.activeMsgValidation.WithLabelValues(topic).Dec()
}

func (m *metricsReporter) IncomingQueueMessage(messageID spectypes.MessageID) {
	m.incomingQueueMessages.WithLabelValues(messageID.String()).Inc()
}

func (m *metricsReporter) OutgoingQueueMessage(messageID spectypes.MessageID) {
	m.outgoingQueueMessages.WithLabelValues(messageID.String()).Inc()
}

func (m *metricsReporter) DroppedQueueMessage(messageID spectypes.MessageID) {
	m.droppedQueueMessages.WithLabelValues(messageID.String()).Inc()
}

func (m *metricsReporter) MessageQueueSize(size int) {
	m.messageQueueSize.WithLabelValues().Set(float64(size))
}

func (m *metricsReporter) MessageQueueCapacity(size int) {
	m.messageQueueCapacity.WithLabelValues().Set(float64(size))
}

func (m *metricsReporter) MessageTimeInQueue(messageID spectypes.MessageID, d time.Duration) {
	m.messageTimeInQueue.WithLabelValues(messageID.String()).Observe(d.Seconds())
}

func (m *metricsReporter) InCommitteeMessage(msgType spectypes.MsgType, decided bool) {
	str := "non-decided"
	if decided {
		str = "decided"
	}
	m.inCommitteeMessages.WithLabelValues(ssvmessage.MsgTypeToString(msgType), str).Inc()
}

func (m *metricsReporter) NonCommitteeMessage(msgType spectypes.MsgType, decided bool) {
	str := "non-decided"
	if decided {
		str = "decided"
	}
	m.nonCommitteeMessages.WithLabelValues(ssvmessage.MsgTypeToString(msgType), str).Inc()
}

func (m *metricsReporter) PeerScore(peerId peer.ID, score float64) {
	m.pubsubPeerScore.WithLabelValues(peerId.String()).Set(score)
}

func (m *metricsReporter) PeerP4Score(peerId peer.ID, score float64) {
	m.pubsubPeerP4Score.WithLabelValues(peerId.String()).Set(score)
}

func (m *metricsReporter) ResetPeerScores() {
	m.pubsubPeerScore.Reset()
	m.pubsubPeerP4Score.Reset()
}

// PeerDisconnected deletes all data about peers which connections have been closed by the current node
func (m *metricsReporter) PeerDisconnected(peerId peer.ID) {
	m.messagesReceivedFromPeer.DeleteLabelValues(peerId.String())
}
