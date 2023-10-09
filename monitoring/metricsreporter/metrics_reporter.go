package metricsreporter

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"

	ssvmessage "github.com/bloxapp/ssv/protocol/v2/message"
)

// TODO: implement all methods

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
)

type MetricsReporter struct {
	logger *zap.Logger
}

func New(opts ...Option) *MetricsReporter {
	mr := &MetricsReporter{
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

	return &MetricsReporter{}
}

func (m *MetricsReporter) SSVNodeHealthy() {
	ssvNodeStatus.Set(ssvNodeHealthy)
}

func (m *MetricsReporter) SSVNodeNotHealthy() {
	ssvNodeStatus.Set(ssvNodeNotHealthy)
}

func (m *MetricsReporter) ExecutionClientReady() {
	executionClientStatus.Set(executionClientOK)
}

func (m *MetricsReporter) ExecutionClientSyncing() {
	executionClientStatus.Set(executionClientSyncing)
}

func (m *MetricsReporter) ExecutionClientFailure() {
	executionClientStatus.Set(executionClientFailure)
}

func (m *MetricsReporter) ExecutionClientLastFetchedBlock(block uint64) {
	executionClientLastFetchedBlock.Set(float64(block))
}

func (m *MetricsReporter) OperatorPublicKey(operatorID spectypes.OperatorID, publicKey []byte) {
	pkHash := fmt.Sprintf("%x", sha256.Sum256(publicKey))
	operatorIndex.WithLabelValues(pkHash, strconv.FormatUint(operatorID, 10)).Set(float64(operatorID))
}

func (m *MetricsReporter) ValidatorInactive(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorInactive)
}
func (m *MetricsReporter) ValidatorNoIndex(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorNoIndex)
}
func (m *MetricsReporter) ValidatorError(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorError)
}
func (m *MetricsReporter) ValidatorReady(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorReady)
}
func (m *MetricsReporter) ValidatorNotActivated(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorNotActivated)
}
func (m *MetricsReporter) ValidatorExiting(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorExiting)
}
func (m *MetricsReporter) ValidatorSlashed(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorSlashed)
}
func (m *MetricsReporter) ValidatorNotFound(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorNotFound)
}
func (m *MetricsReporter) ValidatorPending(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorPending)
}
func (m *MetricsReporter) ValidatorRemoved(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorRemoved)
}
func (m *MetricsReporter) ValidatorUnknown(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorUnknown)
}

func (m *MetricsReporter) EventProcessed(eventName string) {
	eventProcessed.WithLabelValues(eventName).Inc()
}

func (m *MetricsReporter) EventProcessingFailed(eventName string) {
	eventProcessingFailed.WithLabelValues(eventName).Inc()
}

// TODO implement
func (m *MetricsReporter) LastBlockProcessed(uint64) {}
func (m *MetricsReporter) LogsProcessingError(error) {}

func (m *MetricsReporter) MessageAccepted(
	role spectypes.BeaconRole,
	round specqbft.Round,
) {
	messageValidationResult.WithLabelValues(
		messageAccepted,
		"",
		role.String(),
		strconv.FormatUint(uint64(round), 10),
	).Inc()
}

func (m *MetricsReporter) MessageIgnored(
	reason string,
	role spectypes.BeaconRole,
	round specqbft.Round,
) {
	messageValidationResult.WithLabelValues(
		messageIgnored,
		reason,
		role.String(),
		strconv.FormatUint(uint64(round), 10),
	).Inc()
}

func (m *MetricsReporter) MessageRejected(
	reason string,
	role spectypes.BeaconRole,
	round specqbft.Round,
) {
	messageValidationResult.WithLabelValues(
		messageRejected,
		reason,
		role.String(),
		strconv.FormatUint(uint64(round), 10),
	).Inc()
}

func (m *MetricsReporter) SSVMessageType(msgType spectypes.MsgType) {
	messageValidationSSVType.WithLabelValues(ssvmessage.MsgTypeToString(msgType)).Inc()
}

func (m *MetricsReporter) ConsensusMsgType(msgType specqbft.MessageType, signers int) {
	messageValidationConsensusType.WithLabelValues(ssvmessage.QBFTMsgTypeToString(msgType), strconv.Itoa(signers)).Inc()
}

func (m *MetricsReporter) MessageValidationDuration(duration time.Duration, labels ...string) {
	messageValidationDuration.WithLabelValues(labels...).Observe(duration.Seconds())
}

func (m *MetricsReporter) SignatureValidationDuration(duration time.Duration, labels ...string) {
	signatureValidationDuration.WithLabelValues(labels...).Observe(duration.Seconds())
}

func (m *MetricsReporter) MessageSize(size int) {
	messageSize.WithLabelValues().Observe(float64(size))
}

func (m *MetricsReporter) ActiveMsgValidation(topic string) {
	activeMsgValidation.WithLabelValues(topic).Inc()
}

func (m *MetricsReporter) ActiveMsgValidationDone(topic string) {
	activeMsgValidation.WithLabelValues(topic).Dec()
}

func (m *MetricsReporter) IncomingQueueMessage(messageID spectypes.MessageID) {
	incomingQueueMessages.WithLabelValues(messageID.String()).Inc()
}

func (m *MetricsReporter) OutgoingQueueMessage(messageID spectypes.MessageID) {
	outgoingQueueMessages.WithLabelValues(messageID.String()).Inc()
}

func (m *MetricsReporter) DroppedQueueMessage(messageID spectypes.MessageID) {
	droppedQueueMessages.WithLabelValues(messageID.String()).Inc()
}

func (m *MetricsReporter) MessageQueueSize(size int) {
	messageQueueSize.WithLabelValues().Set(float64(size))
}

func (m *MetricsReporter) MessageQueueCapacity(size int) {
	messageQueueCapacity.WithLabelValues().Set(float64(size))
}

func (m *MetricsReporter) MessageTimeInQueue(messageID spectypes.MessageID, d time.Duration) {
	messageTimeInQueue.WithLabelValues(messageID.String()).Observe(d.Seconds())
}

func (m *MetricsReporter) InCommitteeMessage(msgType spectypes.MsgType, decided bool) {
	str := "non-decided"
	if decided {
		str = "decided"
	}
	inCommitteeMessages.WithLabelValues(ssvmessage.MsgTypeToString(msgType), str).Inc()
}

func (m *MetricsReporter) NonCommitteeMessage(msgType spectypes.MsgType, decided bool) {
	str := "non-decided"
	if decided {
		str = "decided"
	}
	nonCommitteeMessages.WithLabelValues(ssvmessage.MsgTypeToString(msgType), str).Inc()
}
