package ethtestutils

import (
	"context"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"testing"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/eth/simulator"
)

func SetFinalizedCheckpointsProducer(sim *simulator.SimulatedBackend) func(ctx context.Context, feed chan<- *eth2apiv1.FinalizedCheckpointEvent) error {
	return func(ctx context.Context, finalizedBlocks chan<- *eth2apiv1.FinalizedCheckpointEvent) error {
		go func() {
			heads := make(chan *ethtypes.Header)
			sub, _ := sim.SubscribeNewHead(ctx, heads)
			defer sub.Unsubscribe()

			for {
				select {
				case <-ctx.Done():
					return
				case <-sub.Err():
					close(finalizedBlocks)
				case _, ok := <-heads:
					if !ok {
						return
					}
					finalizedBlocks <- &eth2apiv1.FinalizedCheckpointEvent{}
				}
			}
		}()
		return nil
	}
}

// CommitBlockAndCheckSequence - Creating a new block and checking the block number has increased sequentially by 1
func CommitBlockAndCheckSequence(
	t *testing.T,
	ctx context.Context,
	sim *simulator.SimulatedBackend,
	lastBlockNumber uint64,
) *ethtypes.Block {
	newBlockHash := sim.Commit()
	currentBlock, err := sim.BlockByHash(ctx, newBlockHash)
	require.NoError(t, err)
	require.Equal(t, lastBlockNumber+1, currentBlock.NumberU64())
	return currentBlock
}
