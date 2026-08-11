package validation

import (
	"math"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/networkconfig"
)

// TestCurrentMaxEncodedPartialSignatureSize pins the fork gate of the pre-decode
// partial-signature size cap: the pre-fork cap applies while boole is unscheduled or more
// than one epoch away, and the post-fork cap applies from one epoch before activation
// (the early flip that protects boundary messages) onward.
func TestCurrentMaxEncodedPartialSignatureSize(t *testing.T) {
	t.Parallel()

	cfgWithBoole := func(booleEpoch phase0.Epoch) *networkconfig.Network {
		ssv := *networkconfig.TestNetwork.SSV
		ssv.Forks = networkconfig.SSVForks{Boole: booleEpoch}
		return &networkconfig.Network{Beacon: networkconfig.TestNetwork.Beacon, SSV: &ssv}
	}
	currentEpoch := networkconfig.TestNetwork.EstimatedCurrentEpoch()

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
			boole: 0,
			want:  maxEncodedPartialSignatureSize,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mv := &messageValidator{netCfg: cfgWithBoole(tc.boole)}
			require.Equal(t, tc.want, mv.currentMaxEncodedPartialSignatureSize())
		})
	}
}
