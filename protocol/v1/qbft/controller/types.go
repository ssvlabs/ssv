package controller

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
)

// Controllers represents a map of controllers by role.
type Controllers map[message.RoleType]IController

// ControllerForIdentifier returns a controller by its identifier.
func (c Controllers) ControllerForIdentifier(identifier message.Identifier) IController {
	role := identifier.GetRoleType()
	return c[role]
}
