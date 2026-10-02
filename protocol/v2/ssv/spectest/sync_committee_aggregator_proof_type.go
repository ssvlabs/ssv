package spectest

import (
	"context"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests/runner/duties/synccommitteeaggregator"

	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
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

// runSyncCommitteeAggProofMessages finishes running test once its duty has been started on runner r (startErr
// is the start's error): it seeds r's beacon node with test's aggregator proof roots, feeds test's messages to
// process, and checks the last error against test's expected error and r's post-duty state root against
// test's.
func runSyncCommitteeAggProofMessages(
	t *testing.T,
	logger *zap.Logger,
	test *synccommitteeaggregator.SyncCommitteeAggregatorProofSpecTest,
	r runner.Runner,
	startErr error,
	process func(context.Context, *zap.Logger, *queue.SSVMessage) error,
) {
	t.Helper()

	overrideStateComparisonForSyncCommitteeAggregatorProofSpecTest(t, test, test.Name)
	if r != nil {
		r.GetBeaconNode().(*protocoltesting.BeaconNodeWrapped).SetSyncCommitteeAggregatorRootHexes(test.ProofRootsMap)
	}

	lastErr := startErr
	for _, msg := range test.Messages {
		dmsg, err := queue.DecodeSignedSSVMessage(msg)
		if err != nil {
			lastErr = err
			continue
		}
		if err := process(t.Context(), logger, dmsg); err != nil {
			lastErr = err
		}
	}
	if test.ExpectedError != "" {
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}

	// Starting the duty may have returned a nil runner (e.g. it errored); guard so the post-root assertion
	// fails legibly instead of panicking on a nil deref.
	require.NotNil(t, r)
	postRoot, err := r.GetStateRoot()
	require.NoError(t, err)
	require.EqualValues(t, test.PostDutyRunnerStateRoot, hex.EncodeToString(postRoot[:]))
}
