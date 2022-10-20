package spectest

import (
	"encoding/hex"
	"github.com/bloxapp/ssv-spec/ssv/spectest/tests/runner/duties/synccommitteeaggregator"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	"github.com/bloxapp/ssv/protocol/v2/ssv/spectest/utils"
	ssv "github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	"github.com/stretchr/testify/require"
	"testing"
)

func RunSyncCommitteeAggProof(t *testing.T, test *synccommitteeaggregator.SyncCommitteeAggregatorProofSpecTest) {
	ks := testingutils.Testing4SharesSet()
	share := testingutils.TestingShare(ks)
	keySet := keySetForShare(share)
	v := ssv.NewValidator(nil, testingutils.NewTestingBeaconNode(), nil, share, nil,
		runner.DutyRunners{
			types.BNRoleAttester:                  utils.AttesterRunner(keySet),
			types.BNRoleProposer:                  utils.ProposerRunner(keySet),
			types.BNRoleAggregator:                utils.AggregatorRunner(keySet),
			types.BNRoleSyncCommittee:             utils.SyncCommitteeRunner(keySet),
			types.BNRoleSyncCommitteeContribution: utils.SyncCommitteeContributionRunner(keySet)})
	r := v.DutyRunners[types.BNRoleSyncCommitteeContribution]
	r.GetBeaconNode().(*testingutils.TestingBeaconNode).SetSyncCommitteeAggregatorRootHexes(test.ProofRootsMap)
	v.Beacon = r.GetBeaconNode()

	lastErr := v.StartDuty(testingutils.TestingSyncCommitteeContributionDuty)
	for _, msg := range test.Messages {
		err := v.ProcessMessage(msg)
		if err != nil {
			lastErr = err
		}
	}

	if len(test.ExpectedError) != 0 {
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}

	// post root
	postRoot, err := r.GetBaseRunner().State.GetRoot()
	require.NoError(t, err)
	require.EqualValues(t, test.PostDutyRunnerStateRoot, hex.EncodeToString(postRoot))
}

func keySetForShare(share *types.Share) *testingutils.TestKeySet {
	if share.Quorum == 5 {
		return testingutils.Testing7SharesSet()
	}
	if share.Quorum == 7 {
		return testingutils.Testing10SharesSet()
	}
	if share.Quorum == 9 {
		return testingutils.Testing13SharesSet()
	}
	return testingutils.Testing4SharesSet()
}
