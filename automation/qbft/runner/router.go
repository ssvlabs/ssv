package runner

import (
	"encoding/hex"
	spectypes "github.com/bloxapp/ssv-spec/types"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/qbft/controller"
)

// Router is an helper router to read messages
type Router struct {
	Logger      *zap.Logger
	Controllers controller.Controllers
}

// Route processes message and routes it to the right controller
func (r *Router) Route(message spectypes.SSVMessage) {
	identifier := message.GetID()
	if err := r.Controllers.ControllerForIdentifier(identifier[:]).ProcessMsg(&message); err != nil {
		r.Logger.Error("failed to process message",
			zap.String("identifier", hex.EncodeToString(identifier[:])))
	}
}
