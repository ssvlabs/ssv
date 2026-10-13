package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

// flakyAggregateBeacon fails GetAggregateAttestation a set number of times before succeeding. Only
// that method is exercised by getAggregateAttestationWithRetry; the embedded nil interface is never
// called.
type flakyAggregateBeacon struct {
	beacon.BeaconNode
	failuresRemaining int
	attestation       ssz.Marshaler
	callCount         int
}

func (b *flakyAggregateBeacon) GetAggregateAttestation(_ context.Context, _ phase0.Slot, _ phase0.CommitteeIndex) (ssz.Marshaler, spec.DataVersion, error) {
	b.callCount++
	if b.failuresRemaining > 0 {
		b.failuresRemaining--
		return nil, spec.DataVersionPhase0, errors.New("transient beacon error")
	}
	return b.attestation, spec.DataVersionPhase0, nil
}

func TestGetAggregateAttestationWithRetry(t *testing.T) {
	att := &phase0.Attestation{}

	t.Run("succeeds after transient failures", func(t *testing.T) {
		b := &flakyAggregateBeacon{failuresRemaining: 2, attestation: att}
		r := &AggregatorCommitteeRunner{beacon: b}
		got, err := r.getAggregateAttestationWithRetry(context.Background(), 1, 2)
		require.NoError(t, err)
		require.Equal(t, att, got)
		require.Equal(t, 3, b.callCount)
	})

	t.Run("returns the last error after exhausting attempts", func(t *testing.T) {
		b := &flakyAggregateBeacon{failuresRemaining: 99, attestation: att}
		r := &AggregatorCommitteeRunner{beacon: b}
		_, err := r.getAggregateAttestationWithRetry(context.Background(), 1, 2)
		require.Error(t, err)
		require.Equal(t, 3, b.callCount) // bounded by the attempt cap
	})

	t.Run("aborts on context cancellation", func(t *testing.T) {
		b := &flakyAggregateBeacon{failuresRemaining: 99, attestation: att}
		r := &AggregatorCommitteeRunner{beacon: b}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := r.getAggregateAttestationWithRetry(ctx, 1, 2)
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, 1, b.callCount) // first attempt runs, then the ctx check bails before the 2nd
	})
}
