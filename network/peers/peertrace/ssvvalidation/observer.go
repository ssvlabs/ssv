package ssvvalidation

import (
	"context"
	"encoding/hex"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/network/peers/peertrace"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/metrics"
	ssvmessage "github.com/ssvlabs/ssv/protocol/v2/message"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/network/peers/peertrace/ssvvalidation"
	observabilityNamespace = "ssv.p2p.highlighted_peer"
)

var (
	meter = otel.Meter(observabilityName)

	highlightedPeerSSVValidationsCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "ssv_validations"),
			metric.WithUnit("{message}"),
			metric.WithDescription("total number of SSV-level validation decisions for messages from configured highlighted peers by outcome and reason")))
)

type Observer struct {
	peerObserver *peertrace.Observer
}

func New(peerObserver *peertrace.Observer) validation.SSVValidationObserver {
	if !peerObserver.Enabled() {
		return nil
	}
	return &Observer{peerObserver: peerObserver}
}

func (o *Observer) ObserveSSVValidation(ctx context.Context, logger *zap.Logger, event validation.SSVValidationEvent) {
	if o == nil || !o.peerObserver.Enabled() {
		return
	}
	match, ok := o.peerObserver.Match(event.PeerID)
	if !ok {
		return
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	label := o.peerObserver.Label()
	messageType := ssvmessage.MsgTypeToString(event.SSVMessageType)
	logFields := []zap.Field{
		zap.Bool("p2p_highlight", true),
		zap.String("p2p_highlight_label", label),
		zap.String("p2p_highlight_event", "ssv_message_validated"),
		zap.String("peer_id", match.ID.String()),
		zap.String("peer_source", match.Source),
		zap.String("ssv_validation_result", event.Outcome),
		zap.String("ssv_validation_reason", event.Reason),
		zap.String("role", event.Role.String()),
		zap.Int32("role_id", int32(event.Role)),
		zap.String("ssv_message_type", messageType),
		zap.Uint64("slot", uint64(event.Slot)),
		zap.String("duty_executor_id", hex.EncodeToString(event.DutyExecutorID)),
		zap.Any("signers", event.Signers),
	}
	qbftMessageType := ""
	if event.Consensus != nil {
		qbftMessageType = ssvmessage.QBFTMsgTypeToString(event.Consensus.QBFTMessageType)
		logFields = append(logFields,
			zap.Uint64("qbft_round", uint64(event.Consensus.Round)),
			zap.String("qbft_message_type", qbftMessageType),
		)
	}
	if match.PublicKeyHex != "" {
		logFields = append(logFields, zap.String("peer_public_key", match.PublicKeyHex))
	}
	if event.Error != "" {
		logFields = append(logFields, zap.String("ssv_validation_error", event.Error))
	}
	logger.Info("p2p highlighted peer ssv validation", logFields...)

	highlightedPeerSSVValidationsCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("ssv.p2p.highlight.label", label),
		attribute.String("ssv.p2p.ssv_validation.result", event.Outcome),
		attribute.String("ssv.p2p.ssv_validation.reason", event.Reason),
		attribute.String("ssv.p2p.message.role", event.Role.String()),
		attribute.String("ssv.p2p.message.type", messageType),
		attribute.String("ssv.p2p.qbft.message.type", qbftMessageType),
		attribute.String("ssv.p2p.peer.id", match.ID.String()),
	))
}
