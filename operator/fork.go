package operator

import (
	"github.com/bloxapp/ssv/operator/validator"
	"time"

	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/prysmaticlabs/prysm/time/slots"
	"go.uber.org/zap"
)

// getForkVersion returns the fork version of the given slot
func (n *operatorNode) getForkVersion(slot uint64) forksprotocol.ForkVersion {
	// TODO: use slot instead of current or remove slot
	currentEpoch := slots.EpochsSinceGenesis(time.Unix(int64(n.ethNetwork.MinGenesisTime()), 0))
	return forksprotocol.GetCurrentForkVersion(currentEpoch)
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
