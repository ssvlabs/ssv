package utils

import (
	"fmt"
	"strconv"
	"strings"
	"time"

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

func FormatDuration(val time.Duration) string {
	return strconv.FormatFloat(val.Seconds(), 'f', 5, 64)
}
