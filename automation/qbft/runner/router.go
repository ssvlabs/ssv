package runner

import (
	"encoding/hex"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/controller"
)

type Router struct {
	Logger      *zap.Logger
	Controllers controller.Controllers
}

func (r *Router) Route(message message.SSVMessage) {
	if err := r.Controllers.ControllerForIdentifier(message.GetIdentifier()).ProcessMsg(&message); err != nil {
		r.Logger.Error("failed to process message",
			zap.String("identifier", hex.EncodeToString(message.GetIdentifier())))
	}
}
