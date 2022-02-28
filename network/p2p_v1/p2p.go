package p2pv1

import (
	"context"
	"github.com/bloxapp/ssv/network/p2p_v1/discovery"
	"github.com/bloxapp/ssv/network/p2p_v1/peers"
	"github.com/bloxapp/ssv/network/p2p_v1/streams"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"go.uber.org/zap"
)

type p2pNetwork struct {
	ctx        context.Context
	logger     *zap.Logger
	cfg        *Config
	host       host.Host
	streamCtrl streams.StreamController
	ids        *identify.IDService
	idx        peers.Index
	disc       discovery.Service
}
