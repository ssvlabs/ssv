package executionclient

import (
	"context"
	"math/big"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/miner"
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

	genesis = &core.Genesis{
		Config:    params.AllEthashProtocolChanges,
		Alloc:     core.GenesisAlloc{testAddr: {Balance: testBalance}},
		ExtraData: []byte("test genesis"),
		Timestamp: 9000,
		BaseFee:   big.NewInt(params.InitialBaseFee),
	}
)

/*
Example contract to test event emission:

	pragma solidity >=0.7.0 <0.9.0;
	contract Callable {
		event Called();
		function Call() public { emit Called(); }
	}
*/

const callableBin = "6080604052348015600f57600080fd5b5060998061001e6000396000f3fe6080604052348015600f57600080fd5b506004361060285760003560e01c806334e2292114602d575b600080fd5b60336035565b005b7f81fab7a4a0aa961db47eefc81f143a5220e8c8495260dd65b1356f1d19d3c7b860405160405180910390a156fea2646970667358221220029436d24f3ac598ceca41d4d712e13ced6d70727f4cdc580667de66d2f51d8b64736f6c63430008010033"
const chainLength = 30

func TestFetchHistoricalLogs(t *testing.T) {
	const testTimeout = 1 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	done := make(chan struct{})
	defer close(done)

	blockStream := make(chan []*ethtypes.Block)
	defer close(blockStream)

	backend, processedStream := newTestBackend(t, done, blockStream, 0)

	// Generate test chain before we read historical logs
	generateInitialTestChain(done, blockStream, chainLength)
	for blocks := range processedStream {
		t.Log("Processed blocks: ", len(blocks))
	}

	// Create JSON-RPC handler
	rpcServer, _ := backend.RPCHandler()
	// Expose handler on a test server with ws open
	httpsrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	defer rpcServer.Stop()
	defer httpsrv.Close()

	// Check if the contract is deployed successfully with a standard eth1 client
	ec, err := backend.Attach()
	require.NoError(t, err)
	cl := ethclient.NewClient(ec)
	receipt, err := cl.TransactionReceipt(ctx, ethcommon.HexToHash("0x0a854d0edf6b757240d5ef2cbfc3fe355525ad1656bf7ce0fbcfa27077a1246a"))
	require.NoError(t, err)
	require.Equal(t, uint64(1), receipt.Status)
	contractCode, err := cl.CodeAt(ctx, receipt.ContractAddress, nil)
	require.NoError(t, err)
	if len(contractCode) == 0 {
		t.Fatal("got code for account that does not have contract code")
	}

	addr := "ws:" + strings.TrimPrefix(httpsrv.URL, "http:")

	logger := zaptest.NewLogger(t)

	const finalizationOffset = 8
	client := New(addr, receipt.ContractAddress, WithLogger(logger), WithFinalizationOffset(finalizationOffset))
	client.Connect(ctx)

	isReady, err := client.IsReady(ctx)
	require.NoError(t, err)
	require.True(t, isReady)

	// Fetch all logs history starting from block 0
	seenLogs := 0
	logs, fetchErrCh, err := client.FetchHistoricalLogs(ctx, 0)
	for log := range logs {
		seenLogs++
		require.NotNil(t, log)
	}

	require.NoError(t, err)
	expectedSeenLogs := chainLength - finalizationOffset - 2 // blocks 0 and 1 don't have logs
	require.Equal(t, expectedSeenLogs, seenLogs)

	select {
	case err := <-fetchErrCh:
		require.NoError(t, err)
	case <-ctx.Done():
		require.Fail(t, "timeout")
	}

	require.NoError(t, client.Close())
	require.NoError(t, backend.Close())
}

func TestStreamLogs(t *testing.T) {
	const testTimeout = 1 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	done := make(chan struct{})
	defer close(done)

	blockStream := make(chan []*ethtypes.Block)
	defer close(blockStream)
	// Create sim instance with a delay between block execution
	delay := time.Millisecond * 10
	backend, processedStream := newTestBackend(t, done, blockStream, delay)

	rpcServer, _ := backend.RPCHandler()
	httpsrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	defer rpcServer.Stop()
	defer httpsrv.Close()

	addr := "ws:" + strings.TrimPrefix(httpsrv.URL, "http:")

	logger := zaptest.NewLogger(t)

	client := New(addr, contractAddr, WithLogger(logger))
	client.Connect(ctx)

	isReady, err := client.IsReady(ctx)
	require.NoError(t, err)
	require.True(t, isReady)

	logs := client.StreamLogs(ctx, 0)
	var wg sync.WaitGroup
	var streamedLogs []ethtypes.Log
	// Receive emitted events
	wg.Add(1)
	go func() {
		defer wg.Done()
		for log := range logs {
			streamedLogs = append(streamedLogs, log)
		}
	}()

	// Generate test chain after a connection to the server.
	// While processing blocks the events will be emitted which is read by subscription
	generateInitialTestChain(done, blockStream, chainLength)
	for blocks := range processedStream {
		t.Log("Processed blocks: ", len(blocks))
	}

	require.NoError(t, client.Close())
	require.NoError(t, backend.Close())
	wg.Wait()
	require.NotEmpty(t, streamedLogs)
}

func newTestBackend(t *testing.T, done <-chan struct{}, blockStream <-chan []*ethtypes.Block, delay time.Duration) (*node.Node, <-chan []*ethtypes.Block) {
	processedStream := make(chan []*ethtypes.Block)
	// Create node
	n, err := node.New(&node.Config{})
	if err != nil {
		t.Fatalf("can't create new node: %v", err)
	}

	// Create Ethereum Service
	config := &ethconfig.Config{Genesis: genesis, Miner: miner.DefaultConfig}
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

	// Start eth1 node
	if err := n.Start(); err != nil {
		t.Fatalf("can't start test node: %v", err)
	}

	go func() {
		defer close(processedStream)
		select {
		case <-done:
			return
		case blocks := <-blockStream:
			if delay != time.Duration(0) {
				for _, block := range blocks {
					if _, err := ethservice.BlockChain().InsertChain([]*ethtypes.Block{block}); err != nil {
						return
					}
					time.Sleep(delay)
				}
			} else {
				if _, err := ethservice.BlockChain().InsertChain(blocks); err != nil {
					return
				}
			}

			processedStream <- blocks
		}
	}()
	return n, processedStream
}

func TestFetchLogsInBatches(t *testing.T) {
	// Create a context with a timeout
	const testTimeout = 1 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Start the test Ethereum backend
	done := make(chan struct{})
	defer close(done)

	blockStream := make(chan []*ethtypes.Block)
	defer close(blockStream)

	backend, processedStream := newTestBackend(t, done, blockStream, 0)

	generateInitialTestChain(done, blockStream, chainLength)

	for blocks := range processedStream {
		t.Log("Processed blocks: ", len(blocks))
	}

	rpcServer, _ := backend.RPCHandler()
	httpsrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	defer rpcServer.Stop()
	defer httpsrv.Close()

	addr := "ws:" + strings.TrimPrefix(httpsrv.URL, "http:")
	logger := zaptest.NewLogger(t)

	client := New(addr, contractAddr, WithLogger(logger), WithLogBatchSize(2))
	client.Connect(ctx)

	// Test the case where fromBlock is greater than toBlock
	logChan, errChan := client.fetchLogsInBatches(ctx, 10, 5)
	select {
	case <-logChan:
		require.Fail(t, "Should not receive log when startBlock > endBlock")
	case err := <-errChan:
		require.ErrorIs(t, err, ErrBadInput)
	case <-ctx.Done():
		require.Fail(t, "fetchLogsInBatches did not return in time when startBlock > endBlock")
	}

	var blockNumbers []uint64

	// Test the case where fromBlock is equal to toBlock
	logChan, errChan = client.fetchLogsInBatches(ctx, 5, 5)
	select {
	case log := <-logChan:
		blockNumbers = append(blockNumbers, log.BlockNumber)
	case err := <-errChan:
		t.Fatalf("fetchLogsInBatches failed: %v", err)
	case <-ctx.Done():
		require.Fail(t, "fetchLogsInBatches did not return in time when fromBlock == toBlock")
	}

	require.Equal(t, []uint64{5}, blockNumbers)
	blockNumbers = nil

	// Test the case where fromBlock < toBlock (normal case)
	logChan, errChan = client.fetchLogsInBatches(ctx, 3, 11)
	for log := range logChan {
		blockNumbers = append(blockNumbers, log.BlockNumber)
	}
	require.Equal(t, []uint64{3, 4, 5, 6, 7, 8, 9, 10, 11}, blockNumbers)

	// Test the case where context is canceled
	canceledCtx, cancel := context.WithCancel(ctx)
	cancel() // cancel the context immediately
	logChan, errChan = client.fetchLogsInBatches(canceledCtx, 0, 5)
	select {
	case <-logChan:
		require.Fail(t, "Should not receive log when context is canceled")
	case err := <-errChan:
		require.Error(t, err, "fetchLogsInBatches should return an error when context is canceled")
	case <-canceledCtx.Done():
	}

	require.NoError(t, backend.Close())
}

// Generate blocks with transactions
func generateInitialTestChain(done <-chan struct{}, blockStream chan []*ethtypes.Block, count int) {
	_, blocks, _ := core.GenerateChainWithGenesis(genesis, ethash.NewFaker(), count, blockGenerator)
	go func() {
		select {
		case <-done:
		case blockStream <- blocks:
		}
	}()
}

func blockGenerator(i int, g *core.BlockGen) {
	g.OffsetTime(5)
	g.SetExtra([]byte("test"))
	if i == 0 {
		return
	}
	// Add contract deployment to the first block
	if i == 1 {
		tx := ethtypes.MustSignNewTx(testKey, ethtypes.LatestSigner(genesis.Config), &ethtypes.LegacyTx{
			Nonce:    uint64(i - 1),
			Value:    big.NewInt(0),
			GasPrice: big.NewInt(params.InitialBaseFee),
			Gas:      300000,
			Data:     ethcommon.FromHex(callableBin),
		})
		g.AddTx(tx)
	} else {
		// Transactions to contract
		tx := ethtypes.MustSignNewTx(testKey, ethtypes.LatestSigner(genesis.Config), &ethtypes.LegacyTx{
			To:       &contractAddr,
			Nonce:    uint64(i - 1),
			Value:    big.NewInt(0),
			GasPrice: big.NewInt(params.InitialBaseFee),
			Gas:      302916,
			// Call to function which emits event
			Data: ethcommon.FromHex("0x34e22921"),
		})
		g.AddTx(tx)
	}
}
