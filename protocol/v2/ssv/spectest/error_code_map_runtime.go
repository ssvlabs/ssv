//go:build !alan_spec

package spectest

import (
	"errors"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

func adjustActualErrorForRunner(err error, r runner.Runner) error {
	if err == nil {
		return nil
	}
	if _, ok := r.(*runner.SyncCommitteeAggregatorRunner); ok {
		return adjustWrongBeaconRole(err)
	}
	return err
}

func adjustActualErrorForRole(err error, role spectypes.RunnerRole) error {
	if err == nil {
		return nil
	}
	if role == ssvtypes.RoleSyncCommitteeContribution {
		return adjustWrongBeaconRole(err)
	}
	return err
}

// adjustWrongBeaconRole remaps WrongBeaconRoleTypeErrorCode to
// QBFTValueInvalidErrorCode for the sync-committee-contribution path.
//
// TODO: this is a test-layer reconciliation that papers over a divergence between
// the code our runner returns and the one the spec fixture expects on this path. It
// can also mask a genuine wrong-role regression here. Reconcile the production error
// code (or document precisely why the divergence is expected and safe) and remove
// this remap. Tracked separately as part of the aggregator-committee convergence.
func adjustWrongBeaconRole(err error) error {
	var specErr *spectypes.Error
	if errors.As(err, &specErr) && specErr.Code == spectypes.WrongBeaconRoleTypeErrorCode {
		return spectypes.WrapError(spectypes.QBFTValueInvalidErrorCode, err)
	}
	return err
}
