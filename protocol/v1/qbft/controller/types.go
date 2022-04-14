package controller

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
)

type Controllers map[message.RoleType]IController

func (c Controllers) ControllerForIdentifier(identifier message.Identifier) IController {
	role := identifier.GetRoleType()
	return c[role]
}
