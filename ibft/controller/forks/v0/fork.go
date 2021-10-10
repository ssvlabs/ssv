package v0

import (
	"github.com/bloxapp/ssv/ibft"
	controller2 "github.com/bloxapp/ssv/ibft/controller"
	"github.com/bloxapp/ssv/ibft/instance/forks"
	v02 "github.com/bloxapp/ssv/ibft/instance/forks/v0"
	"github.com/bloxapp/ssv/ibft/pipeline"
)

type ForkV0 struct {
	controller *controller2.Controller
}

func New() *ForkV0 {
	return &ForkV0{}
}

func (v0 *ForkV0) Apply(controller ibft.Controller) {
	v0.controller = controller.(*controller2.Controller)
}

func (v0 *ForkV0) InstanceFork() forks.Fork {
	return v02.New()
}

func (v0 *ForkV0) ValidateDecidedMsg() pipeline.Pipeline {
	return v0.controller.ValidateDecidedMsgV0()
}
