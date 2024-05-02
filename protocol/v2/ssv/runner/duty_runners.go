package runner

import spectypes "github.com/bloxapp/ssv-spec/types"

// DutyRunners is a map of duty runners mapped by msg id hex.
type DutyRunners map[spectypes.RunnerRole]Runner

// DutyRunnerForMsgID returns a Runner from the provided msg ID, or nil if not found
func (ci DutyRunners) DutyRunnerForMsgID(msgID spectypes.MessageID) Runner {
	role := msgID.GetRoleType()
	return ci[role]
}
