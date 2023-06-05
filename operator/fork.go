package operator

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
)

// getForkVersion returns the fork version of the given slot
func (n *operatorNode) getForkVersion(slot phase0.Slot) forksprotocol.ForkVersion {
	epoch := n.network.Beacon.EstimatedEpochAtSlot(slot)
	return forksprotocol.GetCurrentForkVersion(epoch)
}

// listenForCurrentSlot updates forkVersion and checks if a fork is needed
func (n *operatorNode) setFork(logger *zap.Logger, slot phase0.Slot) {
	currentVersion := n.getForkVersion(slot)
	if currentVersion == n.forkVersion {
		return
	}
	logger = logger.With(zap.String("previousFork", string(n.forkVersion)),
		zap.String("currentFork", string(currentVersion)))
	logger.Info("FORK")

	n.forkVersion = currentVersion

	// set network fork
	netHandler, ok := n.net.(forksprotocol.ForkHandler)
	if !ok {
		logger.Panic("network instance is not a fork handler")
	}
	if err := netHandler.OnFork(logger, currentVersion); err != nil {
		logger.Panic("could not fork network", zap.Error(err))
	}

	// set validator controller fork
	vCtrlHandler, ok := n.validatorsCtrl.(forksprotocol.ForkHandler)
	if !ok {
		logger.Panic("network instance is not a fork handler")
	}
	if err := vCtrlHandler.OnFork(logger, currentVersion); err != nil {
		logger.Panic("could not fork network", zap.Error(err))
	}
}
