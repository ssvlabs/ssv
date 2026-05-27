package peertrace

import (
	"encoding/hex"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.uber.org/zap"
	zapobserver "go.uber.org/zap/zaptest/observer"

	ssvvalidation "github.com/ssvlabs/ssv/message/validation"
)

const attackSimulatorPublicKey = "0x02006c0a9a7e965cb22399987a5a748e90bcc4cb76c461b5d62643c2f2f112055e"

func TestNew_PublicKeyDerivesHighlightedPeer(t *testing.T) {
	observer, err := New(Config{
		Label: "attack-simulator",
		Peers: attackSimulatorPublicKey,
	})
	require.NoError(t, err)
	require.True(t, observer.Enabled())
	require.Equal(t, 1, observer.Count())

	var matched Peer
	for pid := range observer.peers {
		var ok bool
		matched, ok = observer.Match(pid)
		require.True(t, ok)
	}
	require.NotEmpty(t, matched.ID)
	require.Equal(t, "public_key", matched.Source)
	require.Equal(t, attackSimulatorPublicKey, matched.PublicKeyHex)
}

func TestNew_AcceptsMixedPeerList(t *testing.T) {
	pid, err := peer.Decode("12D3KooWGRZpEouTWybB5jDKsVLqYXn3hXyzuTNxti4ghui6u5HE")
	require.NoError(t, err)

	observer, err := New(Config{
		Peers: attackSimulatorPublicKey + ", 12D3KooWGRZpEouTWybB5jDKsVLqYXn3hXyzuTNxti4ghui6u5HE",
	})
	require.NoError(t, err)
	require.Equal(t, 2, observer.Count())

	matched, ok := observer.Match(pid)
	require.True(t, ok)
	require.Equal(t, "peer_id", matched.Source)
}

func TestNew_AcceptsPeerKeyList(t *testing.T) {
	_, pubKey, err := crypto.GenerateSecp256k1Key(nil)
	require.NoError(t, err)
	rawPubKey, err := pubKey.Raw()
	require.NoError(t, err)
	secondPeerKey := "0x" + hex.EncodeToString(rawPubKey)
	secondPeerID, err := peer.IDFromPublicKey(pubKey)
	require.NoError(t, err)

	observer, err := New(Config{
		PeerKeys: attackSimulatorPublicKey + "," + secondPeerKey,
	})
	require.NoError(t, err)
	require.Equal(t, 2, observer.Count())

	matched, ok := observer.Match(secondPeerID)
	require.True(t, ok)
	require.Equal(t, "public_key", matched.Source)
	require.Equal(t, secondPeerKey, matched.PublicKeyHex)
}

func TestNew_DeduplicatesPeerKeysAcrossConfigFields(t *testing.T) {
	observer, err := New(Config{
		Peers:    attackSimulatorPublicKey,
		PeerKeys: attackSimulatorPublicKey,
	})
	require.NoError(t, err)
	require.Equal(t, 1, observer.Count())
}

func TestNew_EmptyConfigDisablesObserver(t *testing.T) {
	observer, err := New(Config{})
	require.NoError(t, err)
	require.Nil(t, observer)
}

func TestObserveValidation_LogsHighlightedPeerAndFields(t *testing.T) {
	observer, err := New(Config{
		Label: "attack-simulator",
		Peers: attackSimulatorPublicKey,
	})
	require.NoError(t, err)

	var pid peer.ID
	for highlightedPeer := range observer.peers {
		pid = highlightedPeer
	}

	core, logs := zapobserver.New(zap.InfoLevel)
	logger := zap.New(core)
	observer.ObserveValidation(t.Context(), logger, pid, "ssv.v2.42", "reject", 128, zap.String("reason", "invalid role"))

	require.Len(t, logs.All(), 1)
	require.Equal(t, "p2p highlighted peer event", logs.All()[0].Message)
	fields := logs.All()[0].ContextMap()
	require.Equal(t, true, fields["p2p_highlight"])
	require.Equal(t, "attack-simulator", fields["p2p_highlight_label"])
	require.Equal(t, "pubsub_message_validated", fields["p2p_highlight_event"])
	require.Equal(t, pid.String(), fields["peer_id"])
	require.Equal(t, "ssv.v2.42", fields["topic"])
	require.Equal(t, "reject", fields["validation_result"])
	require.Equal(t, int64(128), fields["payload_size"])
	require.Equal(t, "invalid role", fields["reason"])
}

func TestObserveSSVValidation_UsesProvidedLogger(t *testing.T) {
	observer, err := New(Config{Peers: attackSimulatorPublicKey})
	require.NoError(t, err)

	var pid peer.ID
	for highlightedPeer := range observer.peers {
		pid = highlightedPeer
	}

	core, logs := zapobserver.New(zap.InfoLevel)
	logger := zap.New(core)
	observer.ObserveSSVValidation(t.Context(), logger, ssvvalidation.SSVValidationEvent{
		PeerID:      pid,
		Outcome:     ssvvalidation.SSVValidationAccepted,
		Reason:      "valid",
		Stage:       ssvvalidation.SSVValidationStageComplete,
		Topic:       "ssv.v2.42",
		PayloadSize: 128,
	})

	require.Len(t, logs.All(), 1)
	require.Equal(t, "p2p highlighted peer ssv validation", logs.All()[0].Message)
	fields := logs.All()[0].ContextMap()
	require.Equal(t, ssvvalidation.SSVValidationAccepted, fields["ssv_validation_result"])
	require.Equal(t, ssvvalidation.SSVValidationStageComplete, fields["ssv_validation_stage"])
	require.Equal(t, "ssv.v2.42", fields["topic"])
	require.Equal(t, int64(128), fields["payload_size"])
}

func TestObservePubsubRejectAndDrop_RecordHighlightedMetrics(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	previousProvider := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(previousProvider)
		require.NoError(t, provider.Shutdown(t.Context()))
	})

	observer, err := New(Config{
		Label: "attack-simulator",
		Peers: attackSimulatorPublicKey,
	})
	require.NoError(t, err)

	var pid peer.ID
	for highlightedPeer := range observer.peers {
		pid = highlightedPeer
	}

	observer.ObservePubsubReject(t.Context(), pid, "ssv.v2.42", "validation failed")
	observer.ObservePubsubDrop(t.Context(), pid, "drop_rpc", "multiple")

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))

	requirePeertraceMetricSum(t, rm, "ssv.p2p.highlighted_peer.pubsub_rejects", map[string]string{
		"ssv.p2p.highlight.label":      "attack-simulator",
		"ssv.p2p.pubsub.topic":         "ssv.v2.42",
		"ssv.p2p.pubsub.reject.reason": "validation failed",
		"ssv.p2p.peer.id":              pid.String(),
	})
	requirePeertraceMetricSum(t, rm, "ssv.p2p.highlighted_peer.pubsub_drops", map[string]string{
		"ssv.p2p.highlight.label":   "attack-simulator",
		"ssv.p2p.pubsub.drop.event": "drop_rpc",
		"ssv.p2p.pubsub.topic":      "multiple",
		"ssv.p2p.peer.id":           pid.String(),
	})
}

func requirePeertraceMetricSum(t *testing.T, rm metricdata.ResourceMetrics, metricName string, attrs map[string]string) {
	t.Helper()

	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metric := range scopeMetrics.Metrics {
			if metric.Name != metricName {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			require.Len(t, sum.DataPoints, 1)
			require.EqualValues(t, 1, sum.DataPoints[0].Value)
			for key, expected := range attrs {
				value, ok := sum.DataPoints[0].Attributes.Value(attribute.Key(key))
				require.True(t, ok)
				require.Equal(t, expected, value.AsString())
			}
			return
		}
	}

	t.Fatalf("%s metric was not collected", metricName)
}
