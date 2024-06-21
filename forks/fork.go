package forks

import (
	"sync/atomic"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

type Epochs struct {
	Alan phase0.Epoch `yaml:"Alan" env:"ALAN_FORK_EPOCH" env-description:"Alan fork epoch"`
}

type Forks struct {
	epochs Epochs
	alan   atomic.Bool
}

type Provider interface {
	IsAlan() bool
}

type EpochReporter interface {
	ReportEpoch(epoch phase0.Epoch)
}

func New(epochs Epochs, initEpoch phase0.Epoch) *Forks {
	f := &Forks{
		epochs: epochs,
	}
	f.ReportEpoch(initEpoch)

	return f
}

func (f *Forks) ReportEpoch(epoch phase0.Epoch) {
	if epoch >= f.epochs.Alan {
		f.alan.Store(true)
	}
}

func (f *Forks) IsAlan() bool {
	return f.alan.Load()
}
