package validation

import (
	"math"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
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
