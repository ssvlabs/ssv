package executionclient

import (
	"context"
	"math/big"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

var (
	// testKey is a private key to use for funding a tester account.
	testKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")

	// testAddr is the Ethereum address of the tester account.
	testAddr = crypto.PubkeyToAddress(testKey.PublicKey)

	testBalance = big.NewInt(2e18)

	contractAddr = ethcommon.HexToAddress("0x3A220f351252089D385b29beca14e27F204c296A")
)

var genesis = &core.Genesis{
	Config:    params.AllEthashProtocolChanges,
	Alloc:     core.GenesisAlloc{testAddr: {Balance: testBalance}},
	ExtraData: []byte("test genesis"),
	Timestamp: 9000,
	BaseFee:   big.NewInt(params.InitialBaseFee),
}

func newTestBackend(t *testing.T, done <-chan interface{}, blockStream <-chan []*types.Block, delay time.Duration) (*node.Node, <-chan []*types.Block) {
	processedStream := make(chan []*types.Block)
	// Create node
	n, err := node.New(&node.Config{})

	if err != nil {
		t.Fatalf("can't create new node: %v", err)
	}
	// Create Ethereum Service
	config := &ethconfig.Config{Genesis: genesis}
	ethservice, err := eth.New(n, config)
	if err != nil {
		t.Fatalf("can't create new ethereum service: %v", err)
	}

	// Add required APIs
	filterSystem := filters.NewFilterSystem(ethservice.APIBackend, filters.Config{})
	n.RegisterAPIs([]rpc.API{{
		Namespace: "eth",
		Service:   filters.NewFilterAPI(filterSystem, false),
	}})

	// Import the test chain.
	if err := n.Start(); err != nil {
		t.Fatalf("can't start test node: %v", err)
	}

	go func() {
		defer close(processedStream)
		select {
		case <-done:
			return
		case blocks := <-blockStream:
			for _, block := range blocks {
				var blocksToProcess []*types.Block
				blocksToProcess = append(blocksToProcess, block)
				if _, err := ethservice.BlockChain().InsertChain(blocksToProcess); err != nil {
					return
				}
				processedStream <- blocksToProcess
				time.Sleep(delay)
			}
		}
	}()
	return n, processedStream
}

func generateInitialTestChain(t *testing.T, done <-chan interface{}, blockStream chan []*types.Block, n int) {
	generate := func(i int, g *core.BlockGen) {
		g.OffsetTime(5)
		g.SetExtra([]byte("test"))
		if i == 0 {
			return
		}
		if i == 1 {
			tx := types.MustSignNewTx(testKey, types.LatestSigner(genesis.Config), &types.LegacyTx{
				Nonce:    uint64(i - 1),
				Value:    big.NewInt(0),
				GasPrice: big.NewInt(params.InitialBaseFee),
				Gas:      100000,
				Data:     ethcommon.FromHex(callableBin),
			})
			g.AddTx(tx)
			// t.Log("Tx hash", tx.Hash().Hex())
		} else {
			to := ethcommon.HexToAddress("0x3A220f351252089D385b29beca14e27F204c296A")
			tx := types.MustSignNewTx(testKey, types.LatestSigner(genesis.Config), &types.LegacyTx{
				To:       &to,
				Nonce:    uint64(i - 1),
				Value:    big.NewInt(0),
				GasPrice: big.NewInt(params.InitialBaseFee),
				Gas:      100000,
				Data:     ethcommon.FromHex("0x34e22921"),
			})
			g.AddTx(tx)
			// t.Log("Tx hash", tx.Hash().Hex())
		}
	}
	_, blocks, _ := core.GenerateChainWithGenesis(genesis, ethash.NewFaker(), n, generate)
	go func() {
		select {
		case <-done:
		case blockStream <- blocks:
		}
	}()
}

/*
Example contract to test event emission:

	pragma solidity >=0.7.0 <0.9.0;
	contract Callable {
		event Called();
		function Call() public { emit Called(); }
	}
*/
const callableAbi = "[{\"anonymous\":false,\"inputs\":[],\"name\":\"Called\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"Call\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]"

const callableBin = "6080604052348015600f57600080fd5b5060998061001e6000396000f3fe6080604052348015600f57600080fd5b506004361060285760003560e01c806334e2292114602d575b600080fd5b60336035565b005b7f81fab7a4a0aa961db47eefc81f143a5220e8c8495260dd65b1356f1d19d3c7b860405160405180910390a156fea2646970667358221220029436d24f3ac598ceca41d4d712e13ced6d70727f4cdc580667de66d2f51d8b64736f6c63430008010033"

func TestFetchHistoricalLogs(t *testing.T) {
	const testTimeout = 100 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	done := make(chan interface{})
	defer close(done)

	blockStream := make(chan []*types.Block)
	defer close(blockStream)

	// Generate test chain.
	generateInitialTestChain(t, done, blockStream, 100)
	backend, processedStream := newTestBackend(t, done, blockStream, time.Microsecond)

	for blocks := range processedStream {
		t.Log("Processed block: ", len(blocks))
	}

	rpcServer, _ := backend.RPCHandler()
	httpsrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	defer rpcServer.Stop()
	defer httpsrv.Close()

	addr := "ws:" + strings.TrimPrefix(httpsrv.URL, "http:")

	logger := zaptest.NewLogger(t)

	client := New(addr, contractAddr, WithLogger(logger))

	require.NoError(t, client.Connect(ctx))

	ready, err := client.IsReady(ctx)
	require.NoError(t, err)
	require.True(t, ready)

	logs, lastBlock, err := client.FetchHistoricalLogs(ctx, 0)
	require.NoError(t, err)
	require.NotEmpty(t, logs)
	require.NotEmpty(t, lastBlock)

	require.NoError(t, client.Close())
	require.NoError(t, backend.Close())

}

func TestStreamLogs(t *testing.T) {
	const testTimeout = 100 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	done := make(chan interface{})
	defer close(done)

	blockStream := make(chan []*types.Block)
	defer close(blockStream)

	backend, processedStream := newTestBackend(t, done, blockStream, time.Microsecond * 50)

	rpcServer, _ := backend.RPCHandler()
	httpsrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	defer rpcServer.Stop()
	defer httpsrv.Close()

	addr := "ws:" + strings.TrimPrefix(httpsrv.URL, "http:")

	logger := zaptest.NewLogger(t)

	client := New(addr, contractAddr, WithLogger(logger))

	require.NoError(t, client.Connect(ctx))

	ready, err := client.IsReady(ctx)
	require.NoError(t, err)
	require.True(t, ready)

	logs := client.StreamLogs(ctx, 0)
	var streamedLogs []types.Log
	go func() {
		for log := range logs {
			streamedLogs = append(streamedLogs, log)
		}
	}()

	// Generate test chain
	generateInitialTestChain(t, done, blockStream, 20)
	for blocks := range processedStream {
		t.Log("Processed block: ", len(blocks))
	}

	require.NotEmpty(t, streamedLogs)

	require.NoError(t, client.Close())
	require.NoError(t, backend.Close())

}
