package types

import (
	"testing"

	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// The registration codes sit in the two slots the spec reserves for them, between
// DecidedWrongInstanceErrorCode and ValidatorExitNoConsensusPhaseErrorCode: the numbers they had before the
// spec removed them, which no spec code takes.
func TestValidatorRegistrationErrorCodes(t *testing.T) {
	require.Equal(t, spectypes.DecidedWrongInstanceErrorCode+1, ValidatorRegistrationNoConsensusPhaseErrorCode)
	require.Equal(t, spectypes.ValidatorExitNoConsensusPhaseErrorCode-1, ValidatorRegistrationNoPostConsensusPhaseErrorCode)
}
