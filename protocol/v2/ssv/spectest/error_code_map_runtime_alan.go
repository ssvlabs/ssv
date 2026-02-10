//go:build alan_spec

package spectest

import (
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
)

func adjustActualErrorForRunner(err error, _ runner.Runner) error {
	return err
}

func adjustActualErrorForRole(err error, _ spectypes.RunnerRole) error {
	return err
}
