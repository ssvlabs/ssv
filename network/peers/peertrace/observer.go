package peertrace

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	ssvvalidation "github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/metrics"
	ssvmessage "github.com/ssvlabs/ssv/protocol/v2/message"
)

const (
	defaultLabel = "highlighted-peer"

	observabilityName      = "github.com/ssvlabs/ssv/network/peers/peertrace"
	observabilityNamespace = "ssv.p2p.highlighted_peer"
)

var (
	meter = otel.Meter(observabilityName)

	highlightedPeerEventsCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "events"),
			metric.WithUnit("{event}"),
			metric.WithDescription("total number of p2p events involving configured highlighted peers")))

	highlightedPeerValidationsCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "validations"),
			metric.WithUnit("{message}"),
			metric.WithDescription("total number of pubsub messages from configured highlighted peers by validation outcome")))

	highlightedPeerSSVValidationsCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "ssv_validations"),
			metric.WithUnit("{message}"),
			metric.WithDescription("total number of SSV-level validation decisions for messages from configured highlighted peers by outcome and reason")))
)

// Config defines peers that should be highlighted in p2p logs and metrics.
type Config struct {
	// Label is attached to every log and metric for this observer.
	Label string
	// Peers is a comma, semicolon, or whitespace separated list of libp2p peer IDs
	// or secp256k1 public keys encoded as hex.
	Peers string
}

type Peer struct {
	ID           peer.ID
	PublicKeyHex string
	Source       string
}

type Observer struct {
	label string
	peers map[peer.ID]Peer
}

func New(cfg Config) (*Observer, error) {
	tokens := splitPeerList(cfg.Peers)
	if len(tokens) == 0 {
		return nil, nil
	}

	label := strings.TrimSpace(cfg.Label)
	if label == "" {
		label = defaultLabel
	}

	observer := &Observer{
		label: label,
		peers: make(map[peer.ID]Peer, len(tokens)),
	}
	for _, token := range tokens {
		tracedPeer, err := parsePeer(token)
		if err != nil {
			return nil, fmt.Errorf("parse highlighted peer %q: %w", token, err)
		}
		observer.peers[tracedPeer.ID] = tracedPeer
	}

	return observer, nil
}

func (o *Observer) Enabled() bool {
	return o != nil && len(o.peers) > 0
}

func (o *Observer) Count() int {
	if o == nil {
		return 0
	}
	return len(o.peers)
}

func (o *Observer) Match(pid peer.ID) (Peer, bool) {
	if o == nil || pid == "" {
		return Peer{}, false
	}
	match, ok := o.peers[pid]
	return match, ok
}

func (o *Observer) Observe(ctx context.Context, logger *zap.Logger, event string, pid peer.ID, fields ...zap.Field) {
	match, ok := o.Match(pid)
	if !ok {
		return
	}

	highlightFields := []zap.Field{
		zap.Bool("p2p_highlight", true),
		zap.String("p2p_highlight_label", o.label),
		zap.String("p2p_highlight_event", event),
		zap.String("peer_id", match.ID.String()),
		zap.String("peer_source", match.Source),
	}
	if match.PublicKeyHex != "" {
		highlightFields = append(highlightFields, zap.String("peer_public_key", match.PublicKeyHex))
	}
	highlightFields = append(highlightFields, fields...)
	logger.Info("p2p highlighted peer event", highlightFields...)

	highlightedPeerEventsCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("ssv.p2p.highlight.label", o.label),
		attribute.String("ssv.p2p.highlight.event", event),
		attribute.String("ssv.p2p.peer.id", match.ID.String()),
	))
}

func (o *Observer) ObserveValidation(
	ctx context.Context,
	logger *zap.Logger,
	pid peer.ID,
	topic string,
	outcome string,
	payloadSize int,
	fields ...zap.Field,
) {
	match, ok := o.Match(pid)
	if !ok {
		return
	}

	validationFields := []zap.Field{
		zap.String("topic", topic),
		zap.String("validation_result", outcome),
		zap.Int("payload_size", payloadSize),
	}
	validationFields = append(validationFields, fields...)
	o.Observe(ctx, logger, "pubsub_message_validated", pid, validationFields...)

	highlightedPeerValidationsCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("ssv.p2p.highlight.label", o.label),
		attribute.String("ssv.p2p.validation.result", outcome),
		attribute.String("ssv.p2p.pubsub.topic", topic),
		attribute.String("ssv.p2p.peer.id", match.ID.String()),
	))
}

func (o *Observer) ObserveSSVValidation(ctx context.Context, event ssvvalidation.SSVValidationEvent) {
	match, ok := o.Match(event.PeerID)
	if !ok {
		return
	}

	logFields := []zap.Field{
		zap.Bool("p2p_highlight", true),
		zap.String("p2p_highlight_label", o.label),
		zap.String("p2p_highlight_event", "ssv_message_validated"),
		zap.String("peer_id", match.ID.String()),
		zap.String("peer_source", match.Source),
		zap.String("ssv_validation_result", event.Outcome),
		zap.String("ssv_validation_reason", event.Reason),
		zap.String("role", event.Role.String()),
		zap.Uint64("role_id", uint64(event.Role)),
		zap.String("ssv_message_type", ssvmessage.MsgTypeToString(event.SSVMessageType)),
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
	zap.L().Info("p2p highlighted peer ssv validation", logFields...)

	highlightedPeerSSVValidationsCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("ssv.p2p.highlight.label", o.label),
		attribute.String("ssv.p2p.ssv_validation.result", event.Outcome),
		attribute.String("ssv.p2p.ssv_validation.reason", event.Reason),
		attribute.String("ssv.p2p.message.role", event.Role.String()),
		attribute.String("ssv.p2p.message.type", ssvmessage.MsgTypeToString(event.SSVMessageType)),
		attribute.String("ssv.p2p.qbft.message.type", qbftMessageType),
		attribute.String("ssv.p2p.peer.id", match.ID.String()),
	))
}

func splitPeerList(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\t' || r == ' '
	})
}

func parsePeer(value string) (Peer, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Peer{}, fmt.Errorf("empty peer")
	}

	if isHexPublicKey(value) {
		return parsePublicKey(value)
	}

	pid, err := peer.Decode(value)
	if err != nil {
		return Peer{}, fmt.Errorf("not a peer ID or secp256k1 public key: %w", err)
	}
	return Peer{
		ID:     pid,
		Source: "peer_id",
	}, nil
}

func isHexPublicKey(value string) bool {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(value, "0x"), "0X")
	if len(trimmed) != 66 && len(trimmed) != 130 {
		return false
	}
	_, err := hex.DecodeString(trimmed)
	return err == nil
}

func parsePublicKey(value string) (Peer, error) {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(value, "0x"), "0X")
	keyBytes, err := hex.DecodeString(trimmed)
	if err != nil {
		return Peer{}, fmt.Errorf("decode public key: %w", err)
	}
	pubKey, err := crypto.UnmarshalSecp256k1PublicKey(keyBytes)
	if err != nil {
		return Peer{}, fmt.Errorf("parse secp256k1 public key: %w", err)
	}
	pid, err := peer.IDFromPublicKey(pubKey)
	if err != nil {
		return Peer{}, fmt.Errorf("derive peer ID: %w", err)
	}
	return Peer{
		ID:           pid,
		PublicKeyHex: "0x" + strings.ToLower(trimmed),
		Source:       "public_key",
	}, nil
}
