package spectest

import (
	"encoding/hex"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests/runner/duties/synccommitteeaggregator"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"
	typescomparable "github.com/ssvlabs/ssv-spec/types/testingutils/comparable"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	ssvtesting "github.com/ssvlabs/ssv/protocol/v2/ssv/testing"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
)

func RunSyncCommitteeAggProof(t *testing.T, test *synccommitteeaggregator.SyncCommitteeAggregatorProofSpecTest) {
	overrideStateComparisonForSyncCommitteeAggregatorProofSpecTest(t, test, test.Name)

	ks := testingutils.Testing4SharesSet()
	share := testingutils.TestingShare(ks, testingutils.TestingValidatorIndex)
	logger := log.TestLogger(t)
	v := ssvtesting.BaseValidator(logger, testingutils.KeySetForShare(share))
	r := v.DutyRunners[spectypes.RoleSyncCommitteeContribution]
	r.GetBeaconNode().(*protocoltesting.BeaconNodeWrapped).SetSyncCommitteeAggregatorRootHexes(test.ProofRootsMap)

	lastErr := v.StartDuty(t.Context(), logger, &testingutils.TestingSyncCommitteeContributionDuty)
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

	// post root
	postRoot, err := r.GetStateRoot()
	require.NoError(t, err)
	require.EqualValues(t, test.PostDutyRunnerStateRoot, hex.EncodeToString(postRoot[:]))
}

func overrideStateComparisonForSyncCommitteeAggregatorProofSpecTest(t *testing.T, test *synccommitteeaggregator.SyncCommitteeAggregatorProofSpecTest, name string) {
	testType := reflect.TypeOf(test).String()
	testType = strings.Replace(testType, "spectest.", "synccommitteeaggregator.", 1)

	runnerState := &runner.State{}
	specDir, err := protocoltesting.GetSpecDir("", filepath.Join("ssv", "spectest"))
	require.NoError(t, err)
	runnerState, err = typescomparable.UnmarshalStateComparison(specDir, name, testType, runnerState)
	require.NoError(t, err)

	root, err := runnerState.GetRoot()
	require.NoError(t, err)

	test.PostDutyRunnerStateRoot = hex.EncodeToString(root[:])
}
