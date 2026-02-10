//go:build !alan_spec

package spectest

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests/runner/duties/synccommitteeaggregator"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	ssvtesting "github.com/ssvlabs/ssv/protocol/v2/ssv/testing"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
)

func RunSyncCommitteeAggProof(t *testing.T, test *synccommitteeaggregator.SyncCommitteeAggregatorProofSpecTest) {
	overrideStateComparisonForSyncCommitteeAggregatorProofSpecTest(t, test, test.Name)

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

	postRoot, err := r.GetStateRoot()
	require.NoError(t, err)
	require.EqualValues(t, test.PostDutyRunnerStateRoot, hex.EncodeToString(postRoot[:]))
}
