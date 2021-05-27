package exporter

import (
	"context"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/network"
	"go.uber.org/zap"
)

type Exporter interface {
	Start(ctx context.Context) error
	Sync(ctx context.Context) error
}

// Options contains options to create the node
type Options struct {
	Logger        *zap.Logger
	ETHNetwork    *core.Network
	Network network.Network
}

type exporter struct {
	logger *zap.Logger
}

func New(opts Options) Exporter {
	e := exporter{
		logger: opts.Logger,
	}
	return &e
}

func (e *exporter) Start(ctx context.Context) error {
	e.logger.Info("exporter.Start()")
	return nil
}

func (e *exporter) Sync(ctx context.Context) error {
	e.logger.Info("exporter.Sync()")
	return nil
}
