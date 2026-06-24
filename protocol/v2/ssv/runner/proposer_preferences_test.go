package runner

import (
	"context"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

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

// The runner validates and aggregates incoming partial signatures against its own frozen preference:
// there is no expected root before executeDuty has built and frozen one, and afterwards it is exactly
// that preference's root under DomainProposerPreferences.
func TestProposerPreferencesRunner_ExpectedPreConsensusRootsAndDomain(t *testing.T) {
	r := &ProposerPreferencesRunner{}

	_, _, err := r.expectedPreConsensusRootsAndDomain()
	require.Error(t, err)

	prefs := &gloas.ProposerPreferences{DependentRoot: phase0.Root{0x01}, ProposalSlot: 5, ValidatorIndex: 7}
	r.proposerPreferences = prefs
	roots, domain, err := r.expectedPreConsensusRootsAndDomain()
	require.NoError(t, err)
	require.Equal(t, []ssz.HashRoot{prefs}, roots)
	require.Equal(t, phase0.DomainType(spectypes.DomainProposerPreferences), domain)
}

// Proposer preferences have no consensus or post-consensus phase; those entry points must reject.
func TestProposerPreferencesRunner_NoConsensusPhases(t *testing.T) {
	r := &ProposerPreferencesRunner{}
	require.Error(t, r.ProcessConsensus(context.Background(), zap.NewNop(), nil))
	require.Error(t, r.ProcessPostConsensus(context.Background(), zap.NewNop(), nil))
}
