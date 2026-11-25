//go:build alan_spec

package spectest

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests/runner/duties/synccommitteeaggregator"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"

	"github.com/ssvlabs/ssv/observability/log"
	ssvtesting "github.com/ssvlabs/ssv/protocol/v2/ssv/testing"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// RunSyncCommitteeAggProof runs an Alan (pre-Boole) sync committee aggregator proof vector through a
// Validator's SyncCommitteeAggregatorRunner, the flow the node runs before the Boole fork.
func RunSyncCommitteeAggProof(t *testing.T, test *synccommitteeaggregator.SyncCommitteeAggregatorProofSpecTest) {
	ks := testingutils.Testing4SharesSet()
	logger := log.TestLogger(t)
	v := ssvtesting.BaseValidator(logger, ks)

	err := v.StartDuty(t.Context(), logger, alanSyncCommitteeContributionDuty(t, v.Share))
	runSyncCommitteeAggProofMessages(t, logger, test, v.DutyRunners[ssvtypes.RoleSyncCommitteeContribution], err, v.ProcessMessage)
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
