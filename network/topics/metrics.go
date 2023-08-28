package topics

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"

	ssvmessage "github.com/bloxapp/ssv/protocol/v2/message"
)

var (
	metricPubsubTrace = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:network:pubsub:trace",
		Help: "Traces of pubsub messages",
	}, []string{"type"})
	metricMsgValidation = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_validation",
		Help: "Message validation results",
	}, []string{"status", "reason"})
	metricMsgValidationSSVType = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_validation_ssv_type",
		Help: "SSV message type",
	}, []string{"ssv_msg_type"})
	metricPubsubOutbound = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:p2p:pubsub:msg:out",
		Help: "Count broadcasted messages",
	}, []string{"topic"})
	metricPubsubInbound = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:p2p:pubsub:msg:in",
		Help: "Count incoming messages",
	}, []string{"topic", "msg_type"})
	metricPubsubActiveMsgValidation = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:p2p:pubsub:msg:val:active",
		Help: "Count active message validation",
	}, []string{"topic"})
	metricPubsubPeerScoreInspect = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:p2p:pubsub:score:inspect",
		Help: "Gauge for negative peer scores",
	}, []string{"pid"})
)

func init() {
	logger := zap.L()
	if err := prometheus.Register(metricPubsubTrace); err != nil {
		logger.Debug("could not register prometheus collector")
	}
	if err := prometheus.Register(metricMsgValidation); err != nil {
		logger.Debug("could not register prometheus collector")
	}
	if err := prometheus.Register(metricPubsubOutbound); err != nil {
		logger.Debug("could not register prometheus collector")
	}
	if err := prometheus.Register(metricPubsubInbound); err != nil {
		logger.Debug("could not register prometheus collector")
	}
	if err := prometheus.Register(metricPubsubActiveMsgValidation); err != nil {
		logger.Debug("could not register prometheus collector")
	}
	if err := prometheus.Register(metricPubsubPeerScoreInspect); err != nil {
		logger.Debug("could not register prometheus collector")
	}
}

type msgValidationStatus string

const (
	validationStatusAccepted msgValidationStatus = "accepted"
	validationStatusIgnored  msgValidationStatus = "ignored"
	validationStatusRejected msgValidationStatus = "rejected"
)

func reportValidationResult(status msgValidationStatus, reason string) {
	metricMsgValidation.WithLabelValues(string(status), reason).Inc()
}

func reportSSVMsgType(ssvMsgType spectypes.MsgType) {
	label := ""

	switch ssvMsgType {
	case spectypes.SSVConsensusMsgType:
		label = "SSVConsensusMsgType"
	case spectypes.SSVPartialSignatureMsgType:
		label = "SSVPartialSignatureMsgType"
	case spectypes.DKGMsgType:
		label = "DKGMsgType"
	case ssvmessage.SSVEventMsgType:
		label = "SSVEventMsgType"
	default:
		label = "unknown"
	}

	metricMsgValidationSSVType.WithLabelValues(label).Inc()
}
