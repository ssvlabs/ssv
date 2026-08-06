//go:build alan_spec

package spectest

import (
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
)

// Identity shims for the alan_spec build — see error_code_map_alan.go. The DEFAULT
// build remaps WrongBeaconRoleTypeErrorCode for the sync-committee-contribution
// path; the alan path does not.
func adjustActualErrorForRunner(err error, _ runner.Runner) error {
	return err
}

func adjustActualErrorForRole(err error, _ spectypes.RunnerRole) error {
	return err
}
