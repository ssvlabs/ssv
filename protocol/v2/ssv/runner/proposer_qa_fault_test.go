package runner

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/qa/faults"
)

func TestGloasBlockVersion(t *testing.T) {
	t.Run("honest", func(t *testing.T) {
		require.Equal(t, networkconfig.DataVersionGloas, gloasBlockVersion())
	})

	t.Run("block-wrong-version stamps fulu", func(t *testing.T) {
		faults.SetForTest(t, faults.BlockWrongVersion)
		require.Equal(t, spec.DataVersionFulu, gloasBlockVersion())
	})
}
