//go:build alan_spec
// +build alan_spec

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

func RunSyncCommitteeAggProof(t *testing.T, test *synccommitteeaggregator.SyncCommitteeAggregatorProofSpecTest) {
	overrideStateComparisonForSyncCommitteeAggregatorProofSpecTest(t, test, test.Name)

	ks := testingutils.Testing4SharesSet()
	logger := log.TestLogger(t)
	committeeMember := testingutils.TestingCommitteeMember(ks)
	v := ssvtesting.BaseValidator(logger, testingutils.KeySetForCommitteeMember(committeeMember))
	r := v.DutyRunners[ssvtypes.RoleSyncCommitteeContribution]
	require.NotNil(t, r, "sync committee runner is missing")
	r.GetBeaconNode().(*protocoltesting.BeaconNodeWrapped).SetSyncCommitteeAggregatorRootHexes(test.ProofRootsMap)

	duty := normalizeAlanSyncCommitteeDuty(t, v.Share.ValidatorPubKey, v.Share.ValidatorIndex)
	lastErr := v.StartDuty(t.Context(), logger, duty)
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

func normalizeAlanSyncCommitteeDuty(
	t *testing.T,
	validatorPubKey spectypes.ValidatorPK,
	validatorIndex phase0.ValidatorIndex,
) *spectypes.ValidatorDuty {
	t.Helper()

	rawDuty := any(testingutils.TestingSyncCommitteeContributionDuty)
	var duty *spectypes.ValidatorDuty
	switch typed := rawDuty.(type) {
	case spectypes.ValidatorDuty:
		duty = &typed
	case *spectypes.ValidatorDuty:
		duty = typed
	case spectypes.AggregatorCommitteeDuty:
		duty = findSyncContributionDutyInAggregator(&typed)
	case *spectypes.AggregatorCommitteeDuty:
		duty = findSyncContributionDutyInAggregator(typed)
	default:
		t.Fatalf("unexpected sync committee duty type %T", rawDuty)
	}

	if duty == nil {
		t.Fatalf("sync committee duty is nil")
	}

	sharePubKey := phase0.BLSPubKey(validatorPubKey)
	if duty.PubKey != sharePubKey || duty.ValidatorIndex != validatorIndex {
		patched := *duty
		patched.PubKey = sharePubKey
		patched.ValidatorIndex = validatorIndex
		return &patched
	}

	return duty
}

func findSyncContributionDutyInAggregator(aggDuty *spectypes.AggregatorCommitteeDuty) *spectypes.ValidatorDuty {
	if aggDuty == nil {
		return nil
	}
	for _, vd := range aggDuty.ValidatorDuties {
		if vd != nil && vd.Type == spectypes.BNRoleSyncCommitteeContribution {
			return vd
		}
	}
	return nil
}
