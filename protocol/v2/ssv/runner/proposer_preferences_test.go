package runner

import (
	"context"
	"fmt"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

type errFeeRecipientProvider struct{}

func (errFeeRecipientProvider) GetFeeRecipient(spectypes.ValidatorPK) (bellatrix.ExecutionAddress, error) {
	return bellatrix.ExecutionAddress{}, fmt.Errorf("no fee recipient")
}

func TestNewProposerPreferencesRunner_RequiresSingleShare(t *testing.T) {
	_, err := NewProposerPreferencesRunner(ProposerPreferencesRunnerOptions{})
	require.Error(t, err)

	r, err := NewProposerPreferencesRunner(ProposerPreferencesRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{
			Share: map[phase0.ValidatorIndex]*spectypes.Share{0: {}},
		},
	})
	require.NoError(t, err)
	require.Equal(t, spectypes.RoleProposerPreferences, r.(*ProposerPreferencesRunner).BaseRunner.RunnerRoleType)
}

// A validator can hold several lookahead proposal slots at once; the dispatcher gives each its own
// sub-runner instead of the single-runner state overwriting/rejecting all but one.
func TestProposerPreferencesRunner_ConcurrentSlotsTracked(t *testing.T) {
	netCfg := networkconfig.TestNetwork
	opts := ProposerPreferencesRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{
			NetworkConfig: netCfg,
			Share:         map[phase0.ValidatorIndex]*spectypes.Share{0: {ValidatorIndex: 0}},
		},
		FeeRecipientProvider: errFeeRecipientProvider{}, // executeDuty fails fast; we assert the per-slot dispatch
	}
	r, err := NewProposerPreferencesRunner(opts)
	require.NoError(t, err)
	disp := r.(*ProposerPreferencesRunner)

	current := netCfg.EstimatedCurrentSlot()
	for _, slot := range []phase0.Slot{current + 10, current + 20} {
		duty := &spectypes.ValidatorDuty{Type: spectypes.BNRoleProposerPreferences, ValidatorIndex: 0, Slot: slot}
		require.NoError(t, disp.StartNewDuty(context.Background(), zap.NewNop(), duty, 1))
	}

	require.Len(t, disp.bySlot, 2) // both slots tracked; neither overwrote or rejected the other
}

// evictPastSlots drops sub-runners whose proposal slot has already passed.
func TestProposerPreferencesRunner_evictPastSlots(t *testing.T) {
	netCfg := networkconfig.TestNetwork
	r, err := NewProposerPreferencesRunner(ProposerPreferencesRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{
			NetworkConfig: netCfg,
			Share:         map[phase0.ValidatorIndex]*spectypes.Share{0: {}},
		},
	})
	require.NoError(t, err)
	disp := r.(*ProposerPreferencesRunner)

	current := netCfg.EstimatedCurrentSlot()
	disp.bySlot[current-1] = newProposerPreferencesSlotRunner(disp.opts)
	disp.bySlot[current+10] = newProposerPreferencesSlotRunner(disp.opts)

	disp.evictPastSlots()

	require.NotContains(t, disp.bySlot, current-1)
	require.Contains(t, disp.bySlot, current+10)
}

// An incoming partial signature for a slot with no sub-runner is retryable, not a hard error.
func TestProposerPreferencesRunner_ProcessPreConsensus_unknownSlot(t *testing.T) {
	r, err := NewProposerPreferencesRunner(ProposerPreferencesRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{Share: map[phase0.ValidatorIndex]*spectypes.Share{0: {}}},
	})
	require.NoError(t, err)

	err = r.ProcessPreConsensus(context.Background(), zap.NewNop(), &spectypes.PartialSignatureMessages{Slot: 999})
	require.Error(t, err)
	require.True(t, IsRetryable(err))
}

// Proposer preferences have no consensus or post-consensus phase; those entry points must reject.
func TestProposerPreferencesRunner_NoConsensusPhases(t *testing.T) {
	r := &ProposerPreferencesRunner{}
	require.Error(t, r.ProcessConsensus(context.Background(), zap.NewNop(), nil))
	require.Error(t, r.ProcessPostConsensus(context.Background(), zap.NewNop(), nil))
}

// The runner validates and aggregates incoming partial signatures against its own frozen preference:
// there is no expected root before executeDuty has built and frozen one, and afterwards it is exactly
// that preference's root under DomainProposerPreferences.
func TestProposerPreferencesSlotRunner_ExpectedPreConsensusRootsAndDomain(t *testing.T) {
	r := &proposerPreferencesSlotRunner{}

	_, _, err := r.expectedPreConsensusRootsAndDomain()
	require.Error(t, err)

	prefs := &gloas.ProposerPreferences{DependentRoot: phase0.Root{0x01}, ProposalSlot: 5, ValidatorIndex: 7}
	r.proposerPreferences = prefs
	roots, domain, err := r.expectedPreConsensusRootsAndDomain()
	require.NoError(t, err)
	require.Equal(t, []ssz.HashRoot{prefs}, roots)
	require.Equal(t, phase0.DomainType(spectypes.DomainProposerPreferences), domain)
}
