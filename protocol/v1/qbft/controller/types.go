package controller

import (
	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/protocol/v1/message"
)

// Controllers represents a map of controllers by role.
type Controllers map[spectypes.BeaconRole]IController

// ControllerForIdentifier returns a controller by its identifier.
func (c Controllers) ControllerForIdentifier(identifier []byte) IController {
	role := message.Identifier(identifier).GetRoleType()
	return c[role]
}
