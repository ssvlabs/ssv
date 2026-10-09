package spectest

import (
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests/runner/duties/synccommitteeaggregator"

	"github.com/ssvlabs/ssv/v2/ibft/storage"
	"github.com/ssvlabs/ssv/v2/protocol/v2/ssv/runner"
)

func overrideStateComparisonForSyncCommitteeAggregatorProofSpecTest(t *testing.T, test *synccommitteeaggregator.SyncCommitteeAggregatorProofSpecTest, name string) {
	testType := reflect.TypeFor[*synccommitteeaggregator.SyncCommitteeAggregatorProofSpecTest]().String()
	testType = strings.Replace(testType, "spectest.", "synccommitteeaggregator.", 1)

	runnerState := &runner.State{}
	runnerState, err := storage.UnmarshalStateComparison("ssv", name, testType, runnerState)
	require.NoError(t, err)

	root, err := runnerState.GetRoot()
	require.NoError(t, err)

	test.PostDutyRunnerStateRoot = hex.EncodeToString(root[:])
}
