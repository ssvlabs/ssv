//go:build alan_spec

package spectest

import (
	"encoding/hex"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests/runner/duties/synccommitteeaggregator"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	ssvtesting "github.com/ssvlabs/ssv/protocol/v2/ssv/testing"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// RunSyncCommitteeAggProof runs an Alan (pre-Boole) sync committee aggregator proof vector through a
// Validator's SyncCommitteeAggregatorRunner, the flow the node runs before the Boole fork.
func RunSyncCommitteeAggProof(t *testing.T, test *synccommitteeaggregator.SyncCommitteeAggregatorProofSpecTest) {
	overrideStateComparisonForSyncCommitteeAggregatorProofSpecTest(t, test, test.Name)

	ks := testingutils.Testing4SharesSet()
	logger := log.TestLogger(t)
	v := ssvtesting.BaseValidator(logger, ks)
	r := v.DutyRunners[ssvtypes.RoleSyncCommitteeContribution]
	require.NotNil(t, r, "sync committee contribution runner is missing")
	r.GetBeaconNode().(*protocoltesting.BeaconNodeWrapped).SetSyncCommitteeAggregatorRootHexes(test.ProofRootsMap)

	lastErr := v.StartDuty(t.Context(), logger, alanSyncCommitteeContributionDuty(t, v.Share))
	for _, msg := range test.Messages {
		dmsg, err := queue.DecodeSignedSSVMessage(msg)
		if err != nil {
			lastErr = err
			continue
		}
		err = v.ProcessMessage(t.Context(), logger, dmsg)
		if err != nil {
			lastErr = err
		}
	}
	if test.ExpectedError != "" {
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}

	postRoot, err := r.GetStateRoot()
	require.NoError(t, err)
	require.EqualValues(t, test.PostDutyRunnerStateRoot, hex.EncodeToString(postRoot[:]))
}

// alanSyncCommitteeContributionDuty returns the validator duty the Alan vectors run for share's validator.
// The pinned spec's TestingSyncCommitteeContributionDuty is an aggregator-committee duty carrying it, but
// for another validator and with sync committee indices [0,129,257] where the Alan vectors use [0,1,2],
// which changes the signing roots.
func alanSyncCommitteeContributionDuty(t *testing.T, share *ssvtypes.SSVShare) *spectypes.ValidatorDuty {
	t.Helper()

	for _, vd := range testingutils.TestingSyncCommitteeContributionDuty.ValidatorDuties {
		if vd.Type == spectypes.BNRoleSyncCommitteeContribution {
			duty := *vd
			duty.PubKey = phase0.BLSPubKey(share.ValidatorPubKey)
			duty.ValidatorIndex = share.ValidatorIndex
			duty.ValidatorSyncCommitteeIndices = []spectypes.ValidatorSyncCommitteeIndex{0, 1, 2}
			return &duty
		}
	}
	t.Fatal("TestingSyncCommitteeContributionDuty has no sync committee contribution duty")
	return nil
}
