package validator

import (
	"testing"
	"time"

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

// TestValidatorRoundTimerUsesConfiguredProposerQuickTimeout joins the two halves that are otherwise
// only tested in isolation: cli validation of ProposerQuickTimeout, and the roundtimer option.
//
// Without this, deleting any assignment along
//
//	CommonOptions -> Options -> Validator.proposerQuickTimeout -> roundtimer.New
//
// leaves every other test green while the operator's configured value is silently discarded and the
// default is armed instead. The preceding ControllerOptions -> CommonOptions hop is outside this
// package and is pinned by TestNewControllerPropagatesProposerQuickTimeout in operator/validator.
func TestValidatorRoundTimerUsesConfiguredProposerQuickTimeout(t *testing.T) {
	netCfg := networkconfig.TestNetwork

	newTimerForRole := func(t *testing.T, configured time.Duration, role spectypes.RunnerRole) *roundtimer.RoundTimer {
		t.Helper()

		// Build through the real options chain rather than setting the field directly, so the
		// CommonOptions -> Options -> NewValidator hops are covered too.
		common := NewCommonOptions(CommonOptions{
			NetworkConfig:        netCfg,
			ProposerQuickTimeout: configured,
		}, 0)
		var pk spectypes.ValidatorPK
		opts := common.NewOptions(
			&ssvtypes.SSVShare{Share: spectypes.Share{ValidatorPubKey: pk}},
			&spectypes.CommitteeMember{},
			runner.ValidatorDutyRunners{},
		)

		v := NewValidator(t.Context(), func() {}, zap.NewNop(), opts)
		id := spectypes.NewValidatorMsgID(netCfg.DomainType, pk, role)

		timer, ok := v.newQBFTRoundTimerF(id)(t.Context(), zap.NewNop(), phase0.Slot(1)).(*roundtimer.RoundTimer)
		require.True(t, ok)
		return timer
	}

	t.Run("configured value reaches the proposer timer", func(t *testing.T) {
		const configured = 1200 * time.Millisecond
		timer := newTimerForRole(t, configured, spectypes.RoleProposer)
		require.Equal(t, configured, timer.RoundTimeout(specqbft.FirstRound))
	})

	t.Run("unset falls back to the SIP-102 default", func(t *testing.T) {
		timer := newTimerForRole(t, 0, spectypes.RoleProposer)
		require.Equal(t, roundtimer.DefaultProposerQuickTimeout, timer.RoundTimeout(specqbft.FirstRound))
	})
}
