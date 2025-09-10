package fields

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func formatRunnerRole(runnerRole spectypes.RunnerRole) string {
	return strings.TrimSuffix(runnerRole.String(), "_RUNNER")
}

func formatCommittee(operators []spectypes.OperatorID) string {
	opids := make([]string, 0, len(operators))
	for _, op := range operators {
		opids = append(opids, fmt.Sprint(op))
	}
	return strings.Join(opids, "_")
}

func formatDuration(val time.Duration) string {
	return strconv.FormatFloat(val.Seconds(), 'f', 5, 64)
}
