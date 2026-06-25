package runner

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
)

// GetStateRoot guards against a nil State (e.g. the duty dispatcher never sets one) instead of panicking.
func TestBaseRunnerGetStateRootNilStateReturnsError(t *testing.T) {
	_, err := (&BaseRunner{}).GetStateRoot()
	require.Error(t, err)
}

func TestVoluntaryExitRunnerDecodePreservesEmbeddedBaseRunnerMethods(t *testing.T) {
	t.Parallel()

	keySet := spectestingutils.Testing4SharesSet()
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)

	runnerIface, err := NewVoluntaryExitRunner(VoluntaryExitRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{
			NetworkConfig: cloneTestNetworkConfig(),
			Share:         map[phase0.ValidatorIndex]*spectypes.Share{share.ValidatorIndex: share},
		},
	})
	require.NoError(t, err)

	runner := runnerIface.(*VoluntaryExitRunner)
	beforeRoot, err := runner.GetRoot()
	require.NoError(t, err)

	data, err := runner.Encode()
	require.NoError(t, err)
	require.Contains(t, string(data), "\"BaseRunner\"")

	var decoded VoluntaryExitRunner
	require.NoError(t, decoded.Decode(data))
	decoded.NetworkConfig = cloneTestNetworkConfig()

	afterRoot, err := decoded.GetRoot()
	require.NoError(t, err)

	require.Equal(t, beforeRoot, afterRoot)
	require.Equal(t, spectypes.RoleVoluntaryExit, decoded.GetRole())
	require.Len(t, decoded.GetShares(), 1)
	require.Equal(t, share.ValidatorIndex, decoded.GetShare().ValidatorIndex)
	require.False(t, decoded.hasDutyRunning())
}

func TestVoluntaryExitRunnerUsesReplacedBaseRunner(t *testing.T) {
	t.Parallel()

	keySet := spectestingutils.Testing4SharesSet()
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)

	runnerIface, err := NewVoluntaryExitRunner(VoluntaryExitRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{
			NetworkConfig: cloneTestNetworkConfig(),
			Share:         map[phase0.ValidatorIndex]*spectypes.Share{share.ValidatorIndex: share},
		},
	})
	require.NoError(t, err)

	runner := runnerIface.(*VoluntaryExitRunner)

	replacementShare := spectestingutils.TestingShare(keySet, share.ValidatorIndex+1)
	runner.BaseRunner = &BaseRunner{
		RunnerRoleType: spectypes.RoleVoluntaryExit,
		NetworkConfig:  cloneTestNetworkConfig(),
		Share: map[phase0.ValidatorIndex]*spectypes.Share{
			replacementShare.ValidatorIndex: replacementShare,
		},
	}

	require.Equal(t, spectypes.RoleVoluntaryExit, runner.GetRole())
	require.Len(t, runner.GetShares(), 1)
	require.Equal(t, replacementShare.ValidatorIndex, runner.GetShare().ValidatorIndex)
}

func TestCommitteeRunnerDecodePreservesEmbeddedBaseRunnerMethods(t *testing.T) {
	t.Parallel()

	env := newCommitteeRunnerEnv(t, []int{spectestingutils.TestingValidatorIndex}, nil, nil)
	env.runner.NetworkConfig = cloneTestNetworkConfig()
	beforeRoot, err := env.runner.GetRoot()
	require.NoError(t, err)

	data, err := env.runner.Encode()
	require.NoError(t, err)
	require.Contains(t, string(data), "\"BaseRunner\"")

	var decoded CommitteeRunner
	require.NoError(t, decoded.Decode(data))
	decoded.NetworkConfig = cloneTestNetworkConfig()

	afterRoot, err := decoded.GetRoot()
	require.NoError(t, err)

	require.Equal(t, beforeRoot, afterRoot)
	require.Equal(t, spectypes.RoleCommittee, decoded.GetRole())
	require.Len(t, decoded.GetShares(), 1)
	require.False(t, decoded.hasDutyRunning())
}
