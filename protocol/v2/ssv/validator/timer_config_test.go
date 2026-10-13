package validator

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// TestValidatorRoundTimerHonorsLegacyProposerRoundTimeout covers the middle hop of the chain
// that wires the cli LegacyProposerRoundTimeout switch into the roundtimer option:
//
//	CommonOptions -> Options -> Validator.legacyProposerRoundTimeout -> roundtimer.New
//
// Without this, deleting any assignment along that hop leaves every other test green while the
// operator's switch is silently discarded and the SIP-102 default is armed instead regardless of
// configuration. The preceding ControllerOptions -> CommonOptions hop is outside this package and
// is pinned by TestNewControllerPropagatesLegacyProposerRoundTimeout in operator/validator; the cli
// LegacyProposerRoundTimeout wiring itself is pinned by Test_newNode_wiresOperatorNode in
// cli/operator.
func TestValidatorRoundTimerHonorsLegacyProposerRoundTimeout(t *testing.T) {
	netCfg := networkconfig.TestNetwork

	newProposerTimer := func(t *testing.T, legacy bool) *roundtimer.RoundTimer {
		t.Helper()

		// Build through the real options chain rather than setting the field directly, so the
		// CommonOptions -> Options -> NewValidator hops are covered too.
		common := NewCommonOptions(CommonOptions{
			NetworkConfig:              netCfg,
			LegacyProposerRoundTimeout: legacy,
		}, 0)
		var pk spectypes.ValidatorPK
		opts := common.NewOptions(
			&ssvtypes.SSVShare{Share: spectypes.Share{ValidatorPubKey: pk}},
			&spectypes.CommitteeMember{},
			runner.ValidatorDutyRunners{},
		)

		v := NewValidator(t.Context(), func() {}, zap.NewNop(), opts)
		id := spectypes.NewValidatorMsgID(netCfg.DomainType, pk, spectypes.RoleProposer)

		timer, ok := v.newQBFTRoundTimerF(id)(t.Context(), zap.NewNop(), phase0.Slot(1)).(*roundtimer.RoundTimer)
		require.True(t, ok)
		return timer
	}

	t.Run("legacy=true arms the pre-SIP-102 budget on the proposer timer", func(t *testing.T) {
		timer := newProposerTimer(t, true)
		require.Equal(t, roundtimer.QuickTimeout, timer.RoundTimeout(specqbft.FirstRound))
	})

	t.Run("legacy=false (zero value) keeps the SIP-102 default", func(t *testing.T) {
		timer := newProposerTimer(t, false)
		require.Equal(t, roundtimer.DefaultProposerQuickTimeout, timer.RoundTimeout(specqbft.FirstRound))
	})
}
