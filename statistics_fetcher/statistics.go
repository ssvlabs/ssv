package main

import (
	"context"
	"fmt"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"math/big"
	"time"
)

func main() {
	ctx := context.Background()

	currentLastFinalizedBlock := uint64(0)
	timestamp := time.Now()

	for {
		fb := getLastFinalizedBlockNumber(ctx)

		if fb > currentLastFinalizedBlock {
			currentLastFinalizedBlock = fb
			now := time.Now()
			fmt.Println("New last finalized block is", fb, "formed in = ", now.Sub(timestamp))
			timestamp = now
		}
		time.Sleep(10 * time.Second)
	}

}

func getLastFinalizedBlockNumber(ctx context.Context) uint64 {
	client, err := ethclient.DialContext(ctx, "ws://bn-h-3.stage.bloxinfra.com:8548/ws")
	if err != nil {
		panic(err)
	}
	header, err := client.HeaderByNumber(ctx, big.NewInt(rpc.FinalizedBlockNumber.Int64()))
	if err != nil {
		fmt.Println(err)
		panic(err)
	}
	client.Close()
	return header.Number.Uint64()
}
