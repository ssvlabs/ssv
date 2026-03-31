package runner

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type dummyValueChecker struct{}

func (dummyValueChecker) CheckValue([]byte) error { return nil }

func TestBaseRunnerDecodePopulatesReceiver(t *testing.T) {
	t.Parallel()

	keySet := spectestingutils.Testing4SharesSet()
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)

	br := &BaseRunner{
		RunnerRoleType: spectypes.RoleProposer,
		NetworkConfig:  cloneTestNetworkConfig(),
		Share: map[phase0.ValidatorIndex]*spectypes.Share{
			share.ValidatorIndex: share,
		},
	}

	data, err := br.Encode()
	require.NoError(t, err)

	var decoded BaseRunner
	require.NoError(t, decoded.Decode(data))

	require.Equal(t, spectypes.RoleProposer, decoded.RunnerRoleType)
	require.Len(t, decoded.Share, 1)
	require.Equal(t, share.ValidatorIndex, decoded.Share[share.ValidatorIndex].ValidatorIndex)
}

func TestAggregatorRunnerDecodeIgnoresValCheck(t *testing.T) {
	t.Parallel()

	keySet := spectestingutils.Testing4SharesSet()
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)

	r, err := NewAggregatorRunner(
		cloneTestNetworkConfig(),
		map[phase0.ValidatorIndex]*spectypes.Share{share.ValidatorIndex: share},
		nil,
		nil,
		nil,
		nil,
		nil,
		dummyValueChecker{},
		0,
	)
	require.NoError(t, err)

	beforeRoot, err := r.GetRoot()
	require.NoError(t, err)

	data, err := r.Encode()
	require.NoError(t, err)
	require.Contains(t, string(data), "\"ValCheck\":null")

	var decoded AggregatorRunner
	require.NoError(t, decoded.Decode(data))
	decoded.NetworkConfig = cloneTestNetworkConfig()

	afterRoot, err := decoded.GetRoot()
	require.NoError(t, err)

	require.Equal(t, beforeRoot, afterRoot)
	require.Equal(t, spectypes.RoleAggregator, decoded.GetRole())
	require.False(t, decoded.HasRunningDuty())
	require.Len(t, decoded.GetShares(), 1)
	require.Nil(t, decoded.ValCheck)
}

func TestProposerRunnerDecodeIgnoresValCheck(t *testing.T) {
	t.Parallel()

	keySet := spectestingutils.Testing4SharesSet()
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)

	runnerIface, err := NewProposerRunner(
		zap.NewNop(),
		cloneTestNetworkConfig(),
		map[phase0.ValidatorIndex]*spectypes.Share{share.ValidatorIndex: share},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		dummyValueChecker{},
		0,
		nil,
		0,
	)
	require.NoError(t, err)

	r := runnerIface.(*ProposerRunner)
	beforeRoot, err := r.GetRoot()
	require.NoError(t, err)

	data, err := r.Encode()
	require.NoError(t, err)
	require.Contains(t, string(data), "\"ValCheck\":null")

	var decoded ProposerRunner
	require.NoError(t, decoded.Decode(data))
	decoded.NetworkConfig = cloneTestNetworkConfig()

	afterRoot, err := decoded.GetRoot()
	require.NoError(t, err)

	require.Equal(t, beforeRoot, afterRoot)
	require.Equal(t, spectypes.RoleProposer, decoded.GetRole())
	require.False(t, decoded.HasRunningDuty())
	require.Len(t, decoded.GetShares(), 1)
	require.Nil(t, decoded.ValCheck)
}

func TestSyncCommitteeAggregatorRunnerDecodeIgnoresValCheck(t *testing.T) {
	t.Parallel()

	keySet := spectestingutils.Testing4SharesSet()
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)

	runnerIface, err := NewSyncCommitteeAggregatorRunner(
		cloneTestNetworkConfig(),
		map[phase0.ValidatorIndex]*spectypes.Share{share.ValidatorIndex: share},
		nil,
		nil,
		nil,
		nil,
		nil,
		dummyValueChecker{},
		0,
	)
	require.NoError(t, err)

	r := runnerIface.(*SyncCommitteeAggregatorRunner)
	beforeRoot, err := r.GetRoot()
	require.NoError(t, err)

	data, err := r.Encode()
	require.NoError(t, err)
	require.Contains(t, string(data), "\"ValCheck\":null")

	var decoded SyncCommitteeAggregatorRunner
	require.NoError(t, decoded.Decode(data))
	decoded.NetworkConfig = cloneTestNetworkConfig()

	afterRoot, err := decoded.GetRoot()
	require.NoError(t, err)

	require.Equal(t, beforeRoot, afterRoot)
	require.Equal(t, spectypes.RoleSyncCommitteeContribution, decoded.GetRole())
	require.False(t, decoded.HasRunningDuty())
	require.Len(t, decoded.GetShares(), 1)
	require.Nil(t, decoded.ValCheck)
}
