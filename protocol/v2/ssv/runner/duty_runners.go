package runner

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

// DutyRunners is a map of duty runners mapped by msg id hex.
type DutyRunners map[types.RunnerRole]Runner

// ByMessageID returns a Runner from the provided msg ID, or nil if not found
func (ci DutyRunners) ByMessageID(msgID spectypes.MessageID) Runner {
	role := msgID.GetRoleType()
	return ci[types.RunnerRoleFromSpec(role)]
}
