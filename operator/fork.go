package operator

import (
	"github.com/bloxapp/ssv/operator/validator"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	types "github.com/prysmaticlabs/eth2-types"
	"go.uber.org/zap"
)

// getForkVersion returns the fork version of the given slot
func (n *operatorNode) getForkVersion(slot uint64) forksprotocol.ForkVersion {
	epoch := n.ethNetwork.EstimatedEpochAtSlot(types.Slot(slot))
	return forksprotocol.GetCurrentForkVersion(epoch)
}

// listenForCurrentSlot updates forkVersion and checks if a fork is needed
func (n *operatorNode) setFork(slot uint64) {
	currentVersion := n.getForkVersion(slot)
	if currentVersion == n.forkVersion {
		return
	}
	logger := n.logger.With(zap.String("previousFork", string(n.forkVersion)),
		zap.String("currentFork", string(currentVersion)))
	logger.Info("FORK")

	n.forkVersion = currentVersion

	// set network fork
	netHandler, ok := n.net.(forksprotocol.ForkHandler)
	if !ok {
		logger.Panic("network instance is not a fork handler")
	}
	if err := netHandler.OnFork(currentVersion); err != nil {
		logger.Panic("could not fork network", zap.Error(err))
	}

	// set validator controller fork
	vCtrlHandler, ok := n.net.(validator.Controller)
	if !ok {
		logger.Panic("network instance is not a fork handler")
	}
	if err := vCtrlHandler.OnFork(currentVersion); err != nil {
		logger.Panic("could not fork network", zap.Error(err))
	}
}
