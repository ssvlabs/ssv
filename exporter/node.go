package exporter

import (
	"context"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/network"
	"go.uber.org/zap"
)

// Exporter represents the main interface of this package
type Exporter interface {
	Start() error
	Sync() error
}

// Options contains options to create the node
type Options struct {
	ApiPort    int `yaml:"ApiPort" env-default:"5001"`

	Logger     *zap.Logger
	ETHNetwork *core.Network

	Eth1Client eth1.Eth1

	Network    network.Network
}

// exporter is the internal implementation of Exporter interface
type exporter struct {
	logger *zap.Logger
	network network.Network
	eth1Client eth1.Eth1

	httpHandlers apiHandlers
}

// New creates a new Exporter instance
func New(opts Options) Exporter {
	e := exporter{
		logger: opts.Logger,
		network: opts.Network,
		eth1Client: opts.Eth1Client,
	}
	e.httpHandlers = newHttpHandlers(&e, opts.ApiPort)
	return &e
}

// Start starts the exporter
func (e *exporter) Start() error {
	e.logger.Info("exporter.Start()")

	return e.httpHandlers.Listen()
}

// Sync takes care of syncing an exporter node
func (e *exporter) Sync() error {
	e.logger.Info("exporter.Sync()")
	return e.eth1Client.Sync()
}

func (e *exporter) getOperators(ctx context.Context) error {
	e.logger.Info("exporter.Sync()")
	return nil
}
