package ssvvalidation

import (
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	zapobserver "go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/network/peers/peertrace"
)

const highlightedPeerID = "12D3KooWGRZpEouTWybB5jDKsVLqYXn3hXyzuTNxti4ghui6u5HE"

func TestNewDisabledObserverReturnsNilInterface(t *testing.T) {
	observer, err := peertrace.New(peertrace.Config{})
	require.NoError(t, err)

	require.Nil(t, New(observer))
}

func TestObserveSSVValidation_UsesProvidedLogger(t *testing.T) {
	pid, err := peer.Decode(highlightedPeerID)
	require.NoError(t, err)
	peerObserver, err := peertrace.New(peertrace.Config{Peers: highlightedPeerID})
	require.NoError(t, err)

	core, logs := zapobserver.New(zap.InfoLevel)
	logger := zap.New(core)
	New(peerObserver).ObserveSSVValidation(t.Context(), logger, validation.SSVValidationEvent{
		PeerID:  pid,
		Outcome: validation.SSVValidationAccepted,
		Reason:  "valid",
	})

	require.Len(t, logs.All(), 1)
	require.Equal(t, "p2p highlighted peer ssv validation", logs.All()[0].Message)
	fields := logs.All()[0].ContextMap()
	require.Equal(t, validation.SSVValidationAccepted, fields["ssv_validation_result"])
	require.Equal(t, uint64(0), fields["role_id"])
}
