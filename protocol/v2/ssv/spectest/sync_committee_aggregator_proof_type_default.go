//go:build !alan_spec

package spectest

import (
	"fmt"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests/runner/duties/synccommitteeaggregator"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	ssvtesting "github.com/ssvlabs/ssv/protocol/v2/ssv/testing"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
)

func RunSyncCommitteeAggProof(t *testing.T, test *synccommitteeaggregator.SyncCommitteeAggregatorProofSpecTest) {
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

	r, _, err := committee.StartDuty(t.Context(), logger, testingutils.TestingSyncCommitteeContributionDuty)
	runSyncCommitteeAggProofMessages(t, logger, test, r, err, committee.ProcessMessage)
}
