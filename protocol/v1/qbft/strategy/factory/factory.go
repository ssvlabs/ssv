package factory

import (
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/qbft/strategy"
	"github.com/bloxapp/ssv/protocol/v1/qbft/strategy/factory/fullnode"
	"github.com/bloxapp/ssv/protocol/v1/qbft/strategy/factory/node"
	"go.uber.org/zap"
)

type Factory struct {
	logger       *zap.Logger
	isFullNode   bool
	decidedStore qbftstorage.DecidedMsgStore
	network      p2pprotocol.Network
}

func NewDecidedFactory(logger *zap.Logger, isFullNode bool, decidedStore qbftstorage.DecidedMsgStore, network p2pprotocol.Network) *Factory {
	return &Factory{
		logger:       logger,
		isFullNode:   isFullNode,
		decidedStore: decidedStore,
		network:      network,
	}
}

func (f *Factory) GetStrategy() strategy.Decided {
	// create decidedStrategy
	if f.isFullNode {
		return fullnode.NewFullNodeStrategy(f.logger, f.decidedStore, f.network)
	} else {
		return node.NewRegularNodeStrategy(f.logger, f.decidedStore, f.network)
	}
}
