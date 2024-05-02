package genesisrunner

import genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"

// DutyRunners is a map of duty runners mapped by msg id hex.
type DutyRunners map[genesisspectypes.BeaconRole]Runner

// DutyRunnerForMsgID returns a Runner from the provided msg ID, or nil if not found
func (ci DutyRunners) DutyRunnerForMsgID(msgID genesisspectypes.MessageID) Runner {
	role := msgID.GetRoleType()
	return ci[role]
}
