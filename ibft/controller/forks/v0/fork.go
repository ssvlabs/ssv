package v0

import (
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/controller"
	"github.com/bloxapp/ssv/ibft/controller/forks"
	instanceFork "github.com/bloxapp/ssv/ibft/instance/forks"
	instanceV0Fork "github.com/bloxapp/ssv/ibft/instance/forks/v0"
	"github.com/bloxapp/ssv/ibft/pipeline"
)

// ForkV0 is the genesis fork for controller
type ForkV0 struct {
	ctrl *controller.Controller
}

// New returns new ForkV0
func New() forks.Fork {
	return &ForkV0{}
}

// SlotTick implementation
func (v0 *ForkV0) SlotTick(slot uint64) {

}

// Apply fork on controller
func (v0 *ForkV0) Apply(ctrl ibft.Controller) {
	v0.ctrl = ctrl.(*controller.Controller)
}

// InstanceFork returns instance fork
func (v0 *ForkV0) InstanceFork() instanceFork.Fork {
	return instanceV0Fork.New()
}

// ValidateDecidedMsg impl
func (v0 *ForkV0) ValidateDecidedMsg() pipeline.Pipeline {
	return v0.ctrl.ValidateDecidedMsgV0()
}
