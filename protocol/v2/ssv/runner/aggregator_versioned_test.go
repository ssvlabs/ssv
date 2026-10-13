package runner

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/electra"
	eth2gloas "github.com/attestantio/go-eth2-client/spec/gloas"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
)

func TestConstructVersionedSignedAggregateAndProof(t *testing.T) {
	t.Parallel()

	sig := phase0.BLSSignature{0xcc}

	t.Run("gloas uses the dedicated Gloas container", func(t *testing.T) {
		t.Parallel()

		msg := &eth2gloas.AggregateAndProof{AggregatorIndex: 64}
		signed, err := constructVersionedSignedAggregateAndProof(&spec.VersionedAggregateAndProof{Version: spec.DataVersionGloas, Gloas: msg}, sig)
		require.NoError(t, err)
		require.Equal(t, spec.DataVersionGloas, signed.Version)
		require.NotNil(t, signed.Gloas)
		require.Same(t, msg, signed.Gloas.Message)
		require.Equal(t, sig, signed.Gloas.Signature)
		require.Nil(t, signed.Electra)
		require.Nil(t, signed.Fulu)
	})

	t.Run("fulu keeps the Electra container", func(t *testing.T) {
		t.Parallel()

		msg := &electra.AggregateAndProof{AggregatorIndex: 64}
		signed, err := constructVersionedSignedAggregateAndProof(&spec.VersionedAggregateAndProof{Version: spec.DataVersionFulu, Fulu: msg}, sig)
		require.NoError(t, err)
		require.Equal(t, spec.DataVersionFulu, signed.Version)
		require.Same(t, msg, signed.Fulu.Message)
		require.Equal(t, sig, signed.Fulu.Signature)
		require.Nil(t, signed.Gloas)
	})

	t.Run("nil inner message errors", func(t *testing.T) {
		t.Parallel()

		versions := []spec.DataVersion{
			spec.DataVersionPhase0,
			spec.DataVersionAltair,
			spec.DataVersionBellatrix,
			spec.DataVersionCapella,
			spec.DataVersionDeneb,
			spec.DataVersionElectra,
			spec.DataVersionFulu,
			spec.DataVersionGloas,
		}
		for _, version := range versions {
			_, err := constructVersionedSignedAggregateAndProof(&spec.VersionedAggregateAndProof{Version: version}, sig)
			require.ErrorContains(t, err, "nil", version.String())
		}
	})

	t.Run("unknown version errors", func(t *testing.T) {
		t.Parallel()

		_, err := constructVersionedSignedAggregateAndProof(&spec.VersionedAggregateAndProof{Version: spec.DataVersion(99)}, sig)
		require.ErrorContains(t, err, "unknown version")
	})
}
