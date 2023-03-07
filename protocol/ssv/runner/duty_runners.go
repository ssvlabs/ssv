package runner

import spectypes "github.com/bloxapp/ssv-spec/types"

// DutyRunners is a map of duty runners mapped by msg id hex.
type DutyRunners map[spectypes.BeaconRole]Runner

// DutyRunnerForMsgID returns a Runner from the provided msg ID, or nil if not found
func (dr DutyRunners) DutyRunnerForMsgID(msgID spectypes.MessageID) Runner {
	role := msgID.GetRoleType()
	return dr[role]
}
