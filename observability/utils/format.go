package utils

import (
	"fmt"
	"strings"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// FormatRunnerRole renders a runner role for duty IDs, log fields, and metric attributes.
// It delegates to ssvtypes.RunnerRoleToString so deprecated Alan roles keep their names
// (see #2955); message.RunnerRoleToString is an independent mapper that must produce the
// same strings — a role added or deprecated in one must be reflected in the other.
func FormatRunnerRole(runnerRole spectypes.RunnerRole) string {
	return strings.TrimSuffix(ssvtypes.RunnerRoleToString(runnerRole), "_RUNNER")
}

func FormatCommittee(operators []spectypes.OperatorID) string {
	opids := make([]string, 0, len(operators))
	for _, op := range operators {
		opids = append(opids, fmt.Sprint(op))
	}
	return strings.Join(opids, "_")
}
