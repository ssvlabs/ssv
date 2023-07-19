package executionclient

import (
	"context"
	"math/big"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/bloxapp/ssv/eth/simulator"
)

var (
	// testKey is a private key to use for funding a tester account.
	testKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	// testAddr is the Ethereum address of the tester account.
	testAddr = crypto.PubkeyToAddress(testKey.PublicKey)
)

func simTestBackend(testAddr ethcommon.Address) *simulator.SimulatedBackend {
	return simulator.NewSimulatedBackend(
		core.GenesisAlloc{
			testAddr: {Balance: big.NewInt(10000000000000000)},
		}, 10000000,
	)
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

const blocksWithLogsLength = 30

func TestFetchHistoricalLogs(t *testing.T) {
	logger := zaptest.NewLogger(t)
	const testTimeout = 1 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Create simulator instance
	sim := simTestBackend(testAddr)

	// Create JSON-RPC handler
	rpcServer, _ := sim.Node.RPCHandler()
	// Expose handler on a test server with ws open
	httpsrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	defer rpcServer.Stop()
	defer httpsrv.Close()
	addr := "ws:" + strings.TrimPrefix(httpsrv.URL, "http:")

	parsed, _ := abi.JSON(strings.NewReader(callableAbi))
	auth, _ := bind.NewKeyedTransactorWithChainID(testKey, big.NewInt(1337))
	contractAddr, _, contract, err := bind.DeployContract(auth, parsed, ethcommon.FromHex(callableBin), sim)
	if err != nil {
		t.Errorf("deploying contract: %v", err)
	}
	sim.Commit()

	// Create a client and connect to the simulator
	const finalizationOffset = 8
	client := New(addr, contractAddr, WithLogger(logger), WithFinalizationOffset(finalizationOffset))
	client.Connect(ctx)

	isReady, err := client.IsReady(ctx)
	require.NoError(t, err)
	require.True(t, isReady)

	// Create blocks with transactions
	for i := 0; i < blocksWithLogsLength; i++ {
		_, err := contract.Transact(auth, "Call")
		if err != nil {
			t.Errorf("transacting: %v", err)
		}
		sim.Commit()
	}

	// Fetch all logs history starting from block 0
	var fetchedLogs []ethtypes.Log
	logs, fetchErrCh, err := client.FetchHistoricalLogs(ctx, 0)
	for log := range logs {
		fetchedLogs = append(fetchedLogs, log)
	}
	require.NoError(t, err)
	require.NotEmpty(t, fetchedLogs)

	expectedSeenLogs := blocksWithLogsLength - finalizationOffset
	require.Equal(t, expectedSeenLogs, len(fetchedLogs))

	select {
	case err := <-fetchErrCh:
		require.NoError(t, err)
	case <-ctx.Done():
		require.Fail(t, "timeout")
	}

	require.NoError(t, client.Close())
	require.NoError(t, sim.Close())
}

func TestStreamLogs(t *testing.T) {
	logger := zaptest.NewLogger(t)
	const testTimeout = 1 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Create sim instance with a delay between block execution
	delay := time.Millisecond * 10
	sim := simTestBackend(testAddr)

	rpcServer, _ := sim.Node.RPCHandler()
	httpsrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	defer rpcServer.Stop()
	defer httpsrv.Close()
	addr := "ws:" + strings.TrimPrefix(httpsrv.URL, "http:")

	// Deploy the contract
	parsed, _ := abi.JSON(strings.NewReader(callableAbi))
	auth, _ := bind.NewKeyedTransactorWithChainID(testKey, big.NewInt(1337))
	contractAddr, _, contract, err := bind.DeployContract(auth, parsed, ethcommon.FromHex(callableBin), sim)
	if err != nil {
		t.Errorf("deploying contract: %v", err)
	}
	sim.Commit()

	// Create a client and connect to the simulator
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

	// Create blocks with transactions
	for i := 0; i < blocksWithLogsLength; i++ {
		_, err := contract.Transact(auth, "Call")
		if err != nil {
			t.Errorf("transacting: %v", err)
		}
		sim.Commit()
		time.Sleep(delay)
	}

	wg.Wait()
	require.NotEmpty(t, streamedLogs)
	require.Equal(t, blocksWithLogsLength, len(streamedLogs))
	require.NoError(t, client.Close())
	require.NoError(t, sim.Close())
}

func TestFetchLogsInBatches(t *testing.T) {
	logger := zaptest.NewLogger(t)
	const testTimeout = 1 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Create simulator instance
	sim := simTestBackend(testAddr)

	rpcServer, _ := sim.Node.RPCHandler()
	httpsrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	defer rpcServer.Stop()
	defer httpsrv.Close()
	addr := "ws:" + strings.TrimPrefix(httpsrv.URL, "http:")

	// Deploy the contract
	parsed, _ := abi.JSON(strings.NewReader(callableAbi))
	auth, _ := bind.NewKeyedTransactorWithChainID(testKey, big.NewInt(1337))
	contractAddr, _, contract, err := bind.DeployContract(auth, parsed, ethcommon.FromHex(callableBin), sim)
	if err != nil {
		t.Errorf("deploying contract: %v", err)
	}
	sim.Commit()

	client := New(addr, contractAddr, WithLogger(logger), WithLogBatchSize(2))
	client.Connect(ctx)

	// Create blocks with transactions
	for i := 0; i < blocksWithLogsLength; i++ {
		_, err := contract.Transact(auth, "Call")
		if err != nil {
			t.Errorf("transacting: %v", err)
		}
		sim.Commit()
	}

	t.Run("startBlock is greater than endBlock", func(t *testing.T) {
		logChan, errChan := client.fetchLogsInBatches(ctx, 10, 5)
		select {
		case <-logChan:
			require.Fail(t, "Should not receive log when startBlock > endBlock")
		case err := <-errChan:
			require.ErrorIs(t, err, ErrBadInput)
		case <-ctx.Done():
			require.Fail(t, "fetchLogsInBatches did not return in time when startBlock > endBlock")
		}
	})

	t.Run("startBlock is same as endBlock", func(t *testing.T) {
		var blockNumbers []uint64

		logChan, errChan := client.fetchLogsInBatches(ctx, 5, 5)
		select {
		case log := <-logChan:
			blockNumbers = append(blockNumbers, log.BlockNumber)
		case err := <-errChan:
			t.Fatalf("fetchLogsInBatches failed: %v", err)
		case <-ctx.Done():
			require.Fail(t, "fetchLogsInBatches did not return in time when fromBlock == toBlock")
		}

		require.Equal(t, []uint64{5}, blockNumbers)
	})

	t.Run("startBlock is less than endBlock", func(t *testing.T) {
		var blockNumbers []uint64

		logChan, errChan := client.fetchLogsInBatches(ctx, 3, 11)
		for log := range logChan {
			blockNumbers = append(blockNumbers, log.BlockNumber)
		}
		require.Equal(t, []uint64{3, 4, 5, 6, 7, 8, 9, 10, 11}, blockNumbers)

		select {
		case err := <-errChan:
			require.NoError(t, err)
		default:
		}
	})

	t.Run("context is canceled", func(t *testing.T) {
		canceledCtx, cancel := context.WithCancel(ctx)
		cancel()

		logChan, errChan := client.fetchLogsInBatches(canceledCtx, 0, 5)
		select {
		case <-logChan:
			require.Fail(t, "Should not receive log when context is canceled")
		case err := <-errChan:
			require.Error(t, err, "fetchLogsInBatches should return an error when context is canceled")
		case <-canceledCtx.Done():
		}
	})

	require.NoError(t, client.Close())
	require.NoError(t, sim.Close())
}

// TestChainReorganizationLogs check that the client receives removed logs correctly.
// Steps:
//  1. Deploy the Callable contract.
//  2. Set up an event subscription.
//  3. Save the current block which will serve as parent for the fork.
//  4. Send a transaction.
//  5. Check that the event was included.
//  6. Fork by using the parent block as ancestor.
//  7. Mine two blocks to trigger a reorg.
//  8. Check that the event was removed.

func TestChainReorganizationLogs(t *testing.T) {
	logger := zaptest.NewLogger(t)
	const testTimeout = 1 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	sim := simTestBackend(testAddr)

	rpcServer, _ := sim.Node.RPCHandler()
	httpsrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	defer rpcServer.Stop()
	defer httpsrv.Close()

	addr := "ws:" + strings.TrimPrefix(httpsrv.URL, "http:")

	// 1.
	parsed, _ := abi.JSON(strings.NewReader(callableAbi))
	auth, _ := bind.NewKeyedTransactorWithChainID(testKey, big.NewInt(1337))
	contractAddr, _, contract, err := bind.DeployContract(auth, parsed, ethcommon.FromHex(callableBin), sim)
	if err != nil {
		t.Errorf("deploying contract: %v", err)
	}
	sim.Commit()

	// Connect the client
	client := New(addr, contractAddr, WithLogger(logger))
	client.Connect(ctx)

	isReady, err := client.IsReady(ctx)
	require.NoError(t, err)
	require.True(t, isReady)
	// 2.
	logs := client.StreamLogs(ctx, 0)
	// 3.
	parent := sim.Blockchain.CurrentBlock()
	// 4.
	for i := 0; i < 10; i++ {
		// Call contract to trigger event emit
		if i == 0 {
			_, err := contract.Transact(auth, "Call")
			if err != nil {
				t.Errorf("transacting: %v", err)
			}
		}
		sim.Commit()
	}
	// 5.
	log := <-logs
	require.NotEmpty(t, log)
	if log.Removed {
		t.Error("Event should be included")
	}
	// 6.
	if err := sim.Fork(context.Background(), parent.Hash()); err != nil {
		t.Errorf("forking: %v", err)
	}
	// 7.
	for i := 0; i < 12; i++ {
		sim.Commit()
	}
	// 8.
	log = <-logs
	require.NotEmpty(t, log)
	if !log.Removed {
		t.Error("Event should be removed")
	}
	if sim.Blockchain.CurrentBlock().Number.Uint64() != uint64(13) {
		t.Error("wrong chain length")
	}
	require.NoError(t, client.Close())
	require.NoError(t, sim.Close())
}
