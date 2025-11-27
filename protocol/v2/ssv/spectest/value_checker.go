package spectest

import (
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

// createValueChecker creates the appropriate real value checker for the runner type.
// This ensures spec tests use the implementation's actual value checking logic.
// Optional signerSource can be provided for cases where the runner's signer is nil
// (e.g., when the runner was deserialized from JSON and signer wasn't serialized).
func createValueChecker(r runner.Runner, signerSource ...runner.Runner) ssv.ValueChecker {
	shares := r.GetShares()
	if len(shares) == 0 {
		return nil
	}

	// Get first share for validator info
	var share *spectypes.Share
	for _, s := range shares {
		share = s
		break
	}

	beaconConfig := networkconfig.TestNetwork.Beacon

	// Helper to get signer from runner or signerSource
	getSigner := func(primary runner.Runner) ekm.BeaconSigner {
		if s := primary.GetSigner(); s != nil {
			return s
		}
		if len(signerSource) > 0 && signerSource[0] != nil {
			return signerSource[0].GetSigner()
		}
		return nil
	}

	switch typedRunner := r.(type) {
	case *runner.ProposerRunner:
		signer := getSigner(typedRunner)
		if signer == nil {
			return nil
		}
		return ssv.NewProposerChecker(
			signer,
			beaconConfig,
			share.ValidatorPubKey,
			share.ValidatorIndex,
			phase0.BLSPubKey(share.SharePubKey),
		)

	case *runner.AggregatorRunner:
		return ssv.NewAggregatorChecker(
			beaconConfig,
			share.ValidatorPubKey,
			share.ValidatorIndex,
		)

	case *runner.SyncCommitteeAggregatorRunner:
		return ssv.NewSyncCommitteeContributionChecker(
			beaconConfig,
			share.ValidatorPubKey,
			share.ValidatorIndex,
		)

	case *runner.CommitteeRunner:
		// Check signer is available
		signer := getSigner(typedRunner)
		if signer == nil {
			return nil
		}

		// Build share public keys
		sharePubKeys := make([]phase0.BLSPubKey, 0, len(shares))
		for _, s := range shares {
			sharePubKeys = append(sharePubKeys, phase0.BLSPubKey(s.SharePubKey))
		}

		// Get slot from state or use testing default
		slot := phase0.Slot(spectestingutils.TestingDutySlot)
		if typedRunner.BaseRunner.State != nil && typedRunner.BaseRunner.State.CurrentDuty != nil {
			slot = typedRunner.BaseRunner.State.CurrentDuty.DutySlot()
		}

		// Construct expected vote from TestingAttestationData (same pattern as testing/runner.go:69-73)
		attData := spectestingutils.TestingAttestationData(spec.DataVersionPhase0)
		expectedVote := &spectypes.BeaconVote{
			BlockRoot: attData.BeaconBlockRoot,
			Source:    attData.Source,
			Target:    attData.Target,
		}

		return ssv.NewVoteChecker(
			signer,
			slot,
			sharePubKeys,
			beaconConfig.EstimatedEpochAtSlot(slot),
			expectedVote,
		)

	default:
		return nil
	}
}
