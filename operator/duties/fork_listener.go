package duties

import (
	"context"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network"
)

type ForkListener struct {
	baseHandler
	p2pnet network.P2PNetwork
}

func NewForkListener(p2pnet network.P2PNetwork) *ForkListener {
	h := &ForkListener{
		p2pnet: p2pnet,
	}
	return h
}

func (f *ForkListener) HandleDuties(ctx context.Context) {
	f.logger.Info("starting fork listener")
	defer f.logger.Info("fork listener exited")

	for {
		select {
		case <-ctx.Done():
			return

		case <-f.ticker.Next():
			slot := f.ticker.Slot()
			if f.network.AlanForked(slot) {
				if err := f.p2pnet.UpdateDomainTypeAtFork(f.logger); err != nil {
					f.logger.Warn("could not update ENR at fork", zap.Error(err))
				}
			}
		case reorgEvent := <-f.reorg:
			if f.network.AlanForked(reorgEvent.Slot) {
				if err := f.p2pnet.UpdateDomainTypeAtFork(f.logger); err != nil {
					f.logger.Warn("could not update ENR at fork", zap.Error(err))
				}
			}
		case <-f.indicesChange:
			slot := f.network.Beacon.EstimatedCurrentSlot()
			if f.network.AlanForked(slot) {
				if err := f.p2pnet.UpdateDomainTypeAtFork(f.logger); err != nil {
					f.logger.Warn("could not update ENR at fork", zap.Error(err))
				}
			}
		}
	}
}

func (h *ForkListener) Name() string {
	return "fork_listener"
}
