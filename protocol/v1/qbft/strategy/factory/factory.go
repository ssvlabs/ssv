package factory

import (
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/qbft/strategy"
	"github.com/bloxapp/ssv/protocol/v1/qbft/strategy/fullnode"
	"github.com/bloxapp/ssv/protocol/v1/qbft/strategy/node"
	"go.uber.org/zap"
)

// Factory is responsible for creating instances of decided strategies
type Factory struct {
	logger       *zap.Logger
	mode         strategy.Mode
	decidedStore qbftstorage.DecidedMsgStore
	network      p2pprotocol.Network
}

// NewDecidedFactory creates a new instance of Factory
func NewDecidedFactory(logger *zap.Logger, mode strategy.Mode, decidedStore qbftstorage.DecidedMsgStore, network p2pprotocol.Network) *Factory {
	return &Factory{
		logger:       logger,
		mode:         mode,
		decidedStore: decidedStore,
		network:      network,
	}
}

// GetStrategy returns the decided strategy
func (f *Factory) GetStrategy() strategy.Decided {
	switch f.mode {
	case strategy.ModeRegularNode:
		return node.NewRegularNodeStrategy(f.logger, f.decidedStore, f.network)
	case strategy.ModeFullNode:
		return fullnode.NewFullNodeStrategy(f.logger, f.decidedStore, f.network)
	default:
		return nil
	}
}
