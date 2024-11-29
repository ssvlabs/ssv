package metricsreporter

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	ssvmessage "github.com/ssvlabs/ssv/protocol/v2/message"
)

const (
	ssvNodeNotHealthy = float64(0)
	ssvNodeHealthy    = float64(1)
)

var (
	ssvNodeStatus = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv_node_status",
		Help: "Status of the operator node",
	})
	operatorIndex = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:exporter:operator_index",
		Help: "operator footprint",
	}, []string{"pubKey", "index"})
	signatureValidationDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_signature_validation_duration_seconds",
		Help:    "Signature validation duration (seconds)",
		Buckets: []float64{0.001, 0.005, 0.010, 0.020, 0.050},
	}, []string{})
	incomingQueueMessages = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_queue_incoming",
		Help: "The amount of message incoming to the validator's msg queue",
	}, []string{"msg_id"})
	outgoingQueueMessages = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_queue_outgoing",
		Help: "The amount of message outgoing from the validator's msg queue",
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
	SSVNodeHealthy()
	SSVNodeNotHealthy()
	OperatorPublicKey(operatorID spectypes.OperatorID, publicKey []byte)
	MessageValidationRSAVerifications()
	SignatureValidationDuration(duration time.Duration, labels ...string)
	IncomingQueueMessage(messageID spectypes.MessageID)
	OutgoingQueueMessage(messageID spectypes.MessageID)
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
}

func New(opts ...Option) MetricsReporter {
	mr := &metricsReporter{
		logger: zap.NewNop(),
	}

	for _, opt := range opts {
		opt(mr)
	}

	return &metricsReporter{}
}

func (m *metricsReporter) SSVNodeHealthy() {
	ssvNodeStatus.Set(ssvNodeHealthy)
}

func (m *metricsReporter) SSVNodeNotHealthy() {
	ssvNodeStatus.Set(ssvNodeNotHealthy)
}

func (m *metricsReporter) OperatorPublicKey(operatorID spectypes.OperatorID, publicKey []byte) {
	pkHash := fmt.Sprintf("%x", sha256.Sum256(publicKey))
	operatorIndex.WithLabelValues(pkHash, strconv.FormatUint(operatorID, 10)).Set(float64(operatorID))
}

func (m *metricsReporter) MessageValidationRSAVerifications() {
	messageValidationRSAVerifications.WithLabelValues().Inc()
}

func (m *metricsReporter) SignatureValidationDuration(duration time.Duration, labels ...string) {
	signatureValidationDuration.WithLabelValues(labels...).Observe(duration.Seconds())
}

func (m *metricsReporter) IncomingQueueMessage(messageID spectypes.MessageID) {
	incomingQueueMessages.WithLabelValues(messageID.String()).Inc()
}

func (m *metricsReporter) OutgoingQueueMessage(messageID spectypes.MessageID) {
	outgoingQueueMessages.WithLabelValues(messageID.String()).Inc()
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

func (m *metricsReporter) InCommitteeMessage(msgType spectypes.MsgType, decided bool) {
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
}
