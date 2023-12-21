package ethtestutils

import (
	"context"
	"testing"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

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
				case <-sub.Err():
					close(finalizedBlocks)
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
