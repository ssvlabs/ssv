package ethtestutils

import (
	"context"

	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/bloxapp/ssv/eth/simulator"
)

func SetFinalizedBlocksProducer(sim *simulator.SimulatedBackend) func(ctx context.Context, finalizedBlocks chan<- uint64) error {
	return func(ctx context.Context, finalizedBlocks chan<- uint64) error {
		go func() {
			heads := make(chan *ethtypes.Header)
			sub, _ := sim.SubscribeNewHead(ctx, heads)
			defer sub.Unsubscribe()

			for {
				select {
				case <-ctx.Done():
					return
				case header, ok := <-heads:
					if !ok {
						return
					}
					finalizedBlocks <- header.Number.Uint64()
				}
			}
		}()
		return nil
	}
}
