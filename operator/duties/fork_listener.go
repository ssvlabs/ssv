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
			epoch := f.network.Beacon.EstimatedEpochAtSlot(slot)
			if f.network.PastAlanForkAtEpoch(epoch) {
				if err := f.p2pnet.UpdateDomainType(f.logger, f.network.DomainTypeAtEpoch(epoch)); err != nil {
					f.logger.Warn("could not update ENR at fork", zap.Error(err))
				}
				// TODO (Alan): find a more elegant solution to stop listening when no more forks.
				return
			}
		}
	}
}

func (h *ForkListener) Name() string {
	return "fork_listener"
}
