//go:build alan_spec

package spectest

import (
	"errors"
	"testing"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

func TestAdjustExpectedErrorCodeAlan(t *testing.T) {
	t.Run("maps removed registration no-consensus-data code", func(t *testing.T) {
		require.Equal(
			t,
			spectypes.ValidatorRegistrationNoConsensusPhaseErrorCode,
			adjustExpectedErrorCode(AlanValidatorRegistrationNoConsensusDataErrorCode),
		)
	})

	t.Run("maps removed exit no-consensus-data code", func(t *testing.T) {
		require.Equal(
			t,
			spectypes.ValidatorExitNoConsensusPhaseErrorCode,
			adjustExpectedErrorCode(AlanValidatorExitNoConsensusDataErrorCode),
		)
	})

	t.Run("maps shifted unknown-duty-role code explicitly", func(t *testing.T) {
		require.Equal(
			t,
			spectypes.UnknownDutyRoleDataErrorCode,
			adjustExpectedErrorCode(AlanUnknownDutyRoleDataErrorCode),
		)
	})

	t.Run("passes through unknown codes", func(t *testing.T) {
		require.Equal(t, 9999, adjustExpectedErrorCode(9999))
	})
}

func TestAdjustActualErrorAlan(t *testing.T) {
	base := spectypes.NewError(spectypes.PostConsensusQuorumWithInvalidSignatures, "invalid signatures")
	adjusted := adjustActualError(base)

	var specErr *spectypes.Error
	require.True(t, errors.As(adjusted, &specErr))
	require.Equal(t, spectypes.ReconstructSignatureErrorCode, specErr.Code)
}
