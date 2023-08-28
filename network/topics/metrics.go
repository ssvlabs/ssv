package topics

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

// TODO: replace with new metrics
var (
	metricPubsubTrace = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:network:pubsub:trace",
		Help: "Traces of pubsub messages",
	}, []string{"type"})
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

type metrics interface {
	MessageAccepted(validatorPK spectypes.ValidatorPK, role spectypes.BeaconRole, slot phase0.Slot, round specqbft.Round)
	MessageIgnored(reason string, validatorPK spectypes.ValidatorPK, role spectypes.BeaconRole, slot phase0.Slot, round specqbft.Round)
	MessageRejected(reason string, validatorPK spectypes.ValidatorPK, role spectypes.BeaconRole, slot phase0.Slot, round specqbft.Round)
	SSVMessageType(msgType spectypes.MsgType)
	MessageValidationDuration(duration time.Duration, labels ...string)
	MessageSize(size int)
}

type nopMetrics struct{}

func (nopMetrics) MessageAccepted(spectypes.ValidatorPK, spectypes.BeaconRole, phase0.Slot, specqbft.Round) {
}
func (nopMetrics) MessageIgnored(string, spectypes.ValidatorPK, spectypes.BeaconRole, phase0.Slot, specqbft.Round) {
}
func (nopMetrics) MessageRejected(string, spectypes.ValidatorPK, spectypes.BeaconRole, phase0.Slot, specqbft.Round) {
}
func (nopMetrics) SSVMessageType(spectypes.MsgType)                   {}
func (nopMetrics) MessageValidationDuration(time.Duration, ...string) {}
func (nopMetrics) MessageSize(int)                                    {}
