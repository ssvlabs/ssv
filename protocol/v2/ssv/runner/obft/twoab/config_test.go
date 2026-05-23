package twoab

import (
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

func TestConfigForCluster_Defaults(t *testing.T) {
	committee := []spectypes.OperatorID{4, 1, 3, 2} // intentionally unsorted
	cfg, err := ConfigForCluster(10, committee, [32]byte{}, nil)
	require.NoError(t, err)
	require.NoError(t, cfg.Validate(), "derived config must pass twoab.Config.Validate")

	require.Equal(t, DefaultK, cfg.K())
	require.Equal(t, DefaultBTT, cfg.BTT)
	require.Equal(t, DefaultRefloodDelay, cfg.SafetyBuffer, "default SafetyBuffer = RefloodDelay")

	// T_0_broadcast = TPhase2a − BTT, and must land within the slot.
	require.Equal(t, cfg.TPhase2a-cfg.BTT, cfg.T0Broadcast())
	require.Greater(t, cfg.T0Broadcast(), time.Duration(0), "T_0_broadcast must be > 0")

	// TPhase2a = RelayCutoff − resolveBudget.
	o := &ConfigOverrides{}
	require.Equal(t, o.relayCutoff()-o.resolveBudget(), cfg.TPhase2a)

	// Leader rotation: layer k → sorted[(slot+k) mod n].
	sorted := []spectypes.OperatorID{1, 2, 3, 4}
	for k := 0; k < cfg.K(); k++ {
		want := sorted[(10+uint64(k))%uint64(len(sorted))]
		require.Equal(t, want, spectypes.OperatorID(cfg.Layers[k].Leader), "layer %d leader", k)
	}
}

func TestConfigForCluster_OutOfEnvelope(t *testing.T) {
	committee := []spectypes.OperatorID{1, 2, 3, 4}
	// BTT too large for the slot budget → derived TPhase2a <= BTT.
	_, err := ConfigForCluster(0, committee, [32]byte{}, &ConfigOverrides{BTT: 2 * time.Second})
	require.ErrorContains(t, err, "out of envelope")
}

func TestConfigForCluster_RejectsBadCluster(t *testing.T) {
	_, err := ConfigForCluster(0, nil, [32]byte{}, nil)
	require.ErrorContains(t, err, "empty committee")

	_, err = ConfigForCluster(0, []spectypes.OperatorID{1, 2, 3}, [32]byte{}, nil)
	require.ErrorContains(t, err, "not 3f+1")

	_, err = ConfigForCluster(0, []spectypes.OperatorID{1, 2, 3, 4}, [32]byte{}, &ConfigOverrides{K: 1})
	require.ErrorContains(t, err, "BFT-liveness minimum")
}

func TestCandidate_Roundtrip(t *testing.T) {
	blinded := []byte{0x01, 0x02, 0x03}
	enc := EncodeCandidate(spec.DataVersionElectra, blinded)

	v, ssz, err := DecodeCandidate(enc)
	require.NoError(t, err)
	require.Equal(t, spec.DataVersionElectra, v)
	require.Equal(t, blinded, ssz)

	_, _, err = DecodeCandidate(nil)
	require.ErrorContains(t, err, "empty value")
}
