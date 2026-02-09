package spectest

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests/runner/duties/synccommitteeaggregator"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"
	typescomparable "github.com/ssvlabs/ssv-spec/types/testingutils/comparable"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	ssvtesting "github.com/ssvlabs/ssv/protocol/v2/ssv/testing"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

func RunSyncCommitteeAggProof(t *testing.T, test *synccommitteeaggregator.SyncCommitteeAggregatorProofSpecTest) {
	overrideStateComparisonForSyncCommitteeAggregatorProofSpecTest(t, test, test.Name)

	if !networkconfig.TestNetwork.BooleForkAtSlot(testingutils.TestingSyncCommitteeContributionDuty.DutySlot()) {
		runSyncCommitteeAggProofAlan(t, test)
		return
	}

	ks := testingutils.Testing4SharesSet()
	share := testingutils.TestingShare(ks, testingutils.TestingValidatorIndex)
	logger := log.TestLogger(t)
	shareMap := map[phase0.ValidatorIndex]*spectypes.Share{
		share.ValidatorIndex: share,
	}
	committee := validator.NewCommittee(
		logger,
		networkconfig.TestNetwork,
		testingutils.TestingCommitteeMember(ks),
		func(
			duty spectypes.Duty,
			shares map[phase0.ValidatorIndex]*spectypes.Share,
			_ []phase0.BLSPubKey,
			_ runner.CommitteeDutyGuard,
		) (runner.Runner, error) {
			switch duty.(type) {
			case *spectypes.CommitteeDuty:
				return ssvtesting.CommitteeRunnerWithShareMap(logger, shares), nil
			case *spectypes.AggregatorCommitteeDuty:
				return ssvtesting.AggregatorCommitteeRunnerWithShareMap(logger, shares), nil
			default:
				return nil, fmt.Errorf("unknown duty type: %T", duty)
			}
		},
		shareMap,
		validator.NewCommitteeDutyGuard(),
	)

	r, _, lastErr := committee.StartDuty(t.Context(), logger, testingutils.TestingSyncCommitteeContributionDuty)
	if r != nil {
		r.GetBeaconNode().(*protocoltesting.BeaconNodeWrapped).SetSyncCommitteeAggregatorRootHexes(test.ProofRootsMap)
	}
	for _, msg := range test.Messages {
		dmsg, err := queue.DecodeSignedSSVMessage(msg)
		if err != nil {
			lastErr = err
			continue
		}
		err = committee.ProcessMessage(t.Context(), logger, dmsg)
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

func runSyncCommitteeAggProofAlan(t *testing.T, test *synccommitteeaggregator.SyncCommitteeAggregatorProofSpecTest) {
	ks := testingutils.Testing4SharesSet()
	logger := log.TestLogger(t)

	v := ssvtesting.BaseValidator(logger, ks)
	r := v.DutyRunners[ssvtypes.RoleSyncCommitteeContribution]
	require.NotNil(t, r, "sync committee runner is missing")

	rawDuty := any(testingutils.TestingSyncCommitteeContributionDuty)
	var duty spectypes.Duty
	switch typed := rawDuty.(type) {
	case spectypes.ValidatorDuty:
		duty = &typed
	case *spectypes.ValidatorDuty:
		duty = typed
	case spectypes.AggregatorCommitteeDuty:
		duty = &typed
	case *spectypes.AggregatorCommitteeDuty:
		duty = typed
	case spectypes.Duty:
		duty = typed
	default:
		t.Fatalf("unexpected sync committee duty type %T", rawDuty)
	}
	if aggDuty, ok := duty.(*spectypes.AggregatorCommitteeDuty); ok {
		var syncDuty *spectypes.ValidatorDuty
		for _, vd := range aggDuty.ValidatorDuties {
			if vd != nil && vd.Type == spectypes.BNRoleSyncCommitteeContribution {
				syncDuty = vd
				break
			}
		}
		if syncDuty == nil {
			t.Fatalf("sync committee duty missing in AggregatorCommitteeDuty")
		}
		duty = syncDuty
	}
	if syncDuty, ok := duty.(*spectypes.ValidatorDuty); ok {
		sharePubKey := phase0.BLSPubKey(v.Share.ValidatorPubKey)
		if syncDuty.PubKey != sharePubKey || syncDuty.ValidatorIndex != v.Share.ValidatorIndex {
			patched := *syncDuty
			patched.PubKey = sharePubKey
			patched.ValidatorIndex = v.Share.ValidatorIndex
			duty = &patched
		}
	}

	lastErr := r.StartNewDuty(t.Context(), logger, duty, v.Operator.GetQuorum())
	r.GetBeaconNode().(*protocoltesting.BeaconNodeWrapped).SetSyncCommitteeAggregatorRootHexes(test.ProofRootsMap)

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

func overrideStateComparisonForSyncCommitteeAggregatorProofSpecTest(t *testing.T, test *synccommitteeaggregator.SyncCommitteeAggregatorProofSpecTest, name string) {
	testType := reflect.TypeOf(test).String()
	testType = strings.Replace(testType, "spectest.", "synccommitteeaggregator.", 1)

	runnerState := &runner.State{}
	specDir, err := storage.GetSpecDir("", filepath.Join("ssv", "spectest"))
	require.NoError(t, err)
	runnerState, err = typescomparable.UnmarshalStateComparison(specDir, name, testType, runnerState)
	require.NoError(t, err)

	root, err := runnerState.GetRoot()
	require.NoError(t, err)

	test.PostDutyRunnerStateRoot = hex.EncodeToString(root[:])
}
