package validation

import (
	"bytes"
	"context"
	"math"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/networkconfig"
)

// testNetworkWithBoole returns a copy of TestNetwork with the Boole fork scheduled at
// booleEpoch.
func testNetworkWithBoole(booleEpoch phase0.Epoch) *networkconfig.Network {
	ssv := *networkconfig.TestNetwork.SSV
	ssv.Forks = networkconfig.SSVForks{Boole: booleEpoch}
	return &networkconfig.Network{Beacon: networkconfig.TestNetwork.Beacon, SSV: &ssv}
}

// testReceivedAtEpoch returns a receivedAt timestamp inside the given epoch. Deriving the
// timestamp from a fixed epoch (rather than sampling the wall clock) keeps the fork-gate
// tests fully deterministic: the epoch the gate computes from receivedAt is the epoch the
// fixtures were built for, no matter when the test runs.
func testReceivedAtEpoch(epoch phase0.Epoch) time.Time {
	return networkconfig.TestNetwork.SlotStartTime(networkconfig.TestNetwork.FirstSlotAtEpoch(epoch))
}

// TestMaxEncodedPartialSignatureSizeAt pins the fork gate of the pre-decode
// partial-signature size cap: the pre-fork cap applies while boole is unscheduled or more
// than one epoch away, and the post-fork cap applies from one epoch before activation
// (the early flip that protects boundary messages) onward.
func TestMaxEncodedPartialSignatureSizeAt(t *testing.T) {
	t.Parallel()

	const currentEpoch = phase0.Epoch(10)
	receivedAt := testReceivedAtEpoch(currentEpoch)

	testCases := []struct {
		name  string
		boole phase0.Epoch
		want  int
	}{
		{
			name:  "unscheduled fork keeps the pre-fork cap",
			boole: math.MaxUint64,
			want:  preForkMaxEncodedPartialSignatureSize,
		},
		{
			name:  "fork two epochs away keeps the pre-fork cap",
			boole: currentEpoch + 2,
			want:  preForkMaxEncodedPartialSignatureSize,
		},
		{
			name:  "cap flips one epoch before activation",
			boole: currentEpoch + 1,
			want:  maxEncodedPartialSignatureSize,
		},
		{
			name:  "active fork uses the post-fork cap",
			boole: currentEpoch,
			want:  maxEncodedPartialSignatureSize,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mv := &messageValidator{netCfg: testNetworkWithBoole(tc.boole)}
			require.Equal(t, tc.want, mv.maxEncodedPartialSignatureSizeAt(receivedAt))
		})
	}
}

// TestPartialSignatureSizeCapEnforcedInValidation drives validatePartialSignatureMessage
// itself (not just the cap selector) with a payload sized between the two caps, guarding
// that the fork-aware cap stays wired into the validation path: pre-fork the payload is
// rejected as too big against the pre-fork cap; with the fork active the same payload
// passes the size gate and only fails later, at decoding.
func TestPartialSignatureSizeCapEnforcedInValidation(t *testing.T) {
	t.Parallel()

	const currentEpoch = phase0.Epoch(10)
	receivedAt := testReceivedAtEpoch(currentEpoch)

	betweenCaps := preForkMaxEncodedPartialSignatureSize + 1
	require.LessOrEqual(t, betweenCaps, maxEncodedPartialSignatureSize)
	signedSSVMessage := &spectypes.SignedSSVMessage{
		SSVMessage: &spectypes.SSVMessage{Data: bytes.Repeat([]byte{1}, betweenCaps)},
	}

	t.Run("pre-fork rejects a payload above the pre-fork cap", func(t *testing.T) {
		t.Parallel()

		mv := &messageValidator{netCfg: testNetworkWithBoole(currentEpoch + 2)}
		_, err := mv.validatePartialSignatureMessage(context.Background(), signedSSVMessage, CommitteeInfo{}, "", "", receivedAt)
		require.ErrorIs(t, err, ErrSSVDataTooBig)

		var valErr Error
		require.ErrorAs(t, err, &valErr)
		require.Equal(t, preForkMaxEncodedPartialSignatureSize, valErr.want, "rejection must be against the pre-fork cap")
	})

	t.Run("active fork lets the same payload past the size gate", func(t *testing.T) {
		t.Parallel()

		mv := &messageValidator{netCfg: testNetworkWithBoole(currentEpoch)}
		_, err := mv.validatePartialSignatureMessage(context.Background(), signedSSVMessage, CommitteeInfo{}, "", "", receivedAt)
		require.NotErrorIs(t, err, ErrSSVDataTooBig)
		// The garbage payload fails at the next step, decoding — proof the size gate
		// (not the content) made the difference between the two cases.
		require.ErrorIs(t, err, ErrUndecodableMessageData)
	})
}
