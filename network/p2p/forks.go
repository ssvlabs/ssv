package p2pv1

import (
	"fmt"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// OnFork handles a fork event, it will close the current p2p network
// and recreate it with while preserving previous state (active validators)
// NOTE: ths method MUST be called once per fork version, otherwise we are just restarting the network
func (n *p2pNetwork) OnFork(forkVersion forksprotocol.ForkVersion) error {
	logger := n.logger.With(zap.String("where", "OnFork"))
	logger.Info("forking network")
	return errors.New(fmt.Sprintf("no handler for fork - %s", forkVersion.String()))
}
