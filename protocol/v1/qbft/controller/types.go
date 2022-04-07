package controller

import (
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
)

type Controllers map[beaconprotocol.RoleType]IController

func (c Controllers) ControllerForIdentifier(identifier message.Identifier) IController {
	role := identifier.GetRoleType()
	return c[role]
}
