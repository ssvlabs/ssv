package runner

import spectypes "github.com/ssvlabs/ssv-spec/types"

// ValidatorDutyRunners is a map of duty runners mapped by msg id hex.
type ValidatorDutyRunners map[spectypes.RunnerRole]Runner

// DutyRunnerForMsgID returns a Runner from the provided msg ID, or nil if not found
func (r ValidatorDutyRunners) DutyRunnerForMsgID(msgID spectypes.MessageID) Runner {
	role := msgID.GetRoleType()
	return r[role]
}
