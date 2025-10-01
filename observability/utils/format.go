package utils

import (
	"fmt"
	"strings"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func FormatRunnerRole(runnerRole spectypes.RunnerRole) string {
	return strings.TrimSuffix(runnerRole.String(), "_RUNNER")
}

func FormatCommittee(operators []spectypes.OperatorID) string {
	opids := make([]string, 0, len(operators))
	for _, op := range operators {
		opids = append(opids, fmt.Sprint(op))
	}
	return strings.Join(opids, "_")
}
