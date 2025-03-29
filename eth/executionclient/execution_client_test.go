package executionclient

import (
	"context"
	"math/big"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient/simulated"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/ssvlabs/ssv/eth/simulator"
	"github.com/ssvlabs/ssv/eth/simulator/simcontract"
)

var (
	// testKey is a private key to use for funding a tester account.
	testKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	// testAddr is the Ethereum address of the tester account.
	testAddr = crypto.PubkeyToAddress(testKey.PublicKey)
)

func simTestBackend(testAddr ethcommon.Address) *simulator.Backend {
	return simulator.NewBackend(
		ethtypes.GenesisAlloc{
			testAddr: {Balance: big.NewInt(10000000000000000)},
		},
		simulated.WithBlockGasLimit(10000000),
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
	rpcServer, _ := sim.Node().RPCHandler()
	// Expose handler on a test server with ws open
	httpsrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	defer rpcServer.Stop()
	defer httpsrv.Close()
	addr := httpToWebSocketURL(httpsrv.URL)

	parsed, _ := abi.JSON(strings.NewReader(callableAbi))
	auth, _ := bind.NewKeyedTransactorWithChainID(testKey, big.NewInt(1337))
	contractAddr, _, contract, err := bind.DeployContract(auth, parsed, ethcommon.FromHex(callableBin), sim.Client())
	if err != nil {
		t.Errorf("deploying contract: %v", err)
	}
	sim.Commit()

	// Create a client and connect to the simulator
	const followDistance = 8
	client, err := New(
		ctx,
		addr,
		contractAddr,
		WithLogger(logger),
		WithFollowDistance(followDistance),
		WithConnectionTimeout(2*time.Second),
		WithReconnectionInitialInterval(2*time.Second),
	)
	require.NoError(t, err)

	err = client.Healthy(ctx)
	require.NoError(t, err)

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
	for block := range logs {
		fetchedLogs = append(fetchedLogs, block.Logs...)
	}
	require.NoError(t, err)
	require.NotEmpty(t, fetchedLogs)

	expectedSeenLogs := blocksWithLogsLength - followDistance
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
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	const testTimeout = 2 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Create sim instance with a delay between block execution
	delay := time.Millisecond * 10
	sim := simTestBackend(testAddr)

	rpcServer, _ := sim.Node().RPCHandler()
	httpsrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	defer rpcServer.Stop()
	defer httpsrv.Close()
	addr := httpToWebSocketURL(httpsrv.URL)

	// Deploy the contract
	parsed, _ := abi.JSON(strings.NewReader(callableAbi))
	auth, _ := bind.NewKeyedTransactorWithChainID(testKey, big.NewInt(1337))
	contractAddr, _, contract, err := bind.DeployContract(auth, parsed, ethcommon.FromHex(callableBin), sim.Client())
	if err != nil {
		t.Errorf("deploying contract: %v", err)
	}
	sim.Commit()

	// Create a client and connect to the simulator
	const followDistance = 2
	client, err := New(ctx, addr, contractAddr, WithLogger(logger), WithFollowDistance(followDistance))
	require.NoError(t, err)

	err = client.Healthy(ctx)
	require.NoError(t, err)

	logs := client.StreamLogs(ctx, 0)
	var streamedLogs []ethtypes.Log
	var streamedLogsCount atomic.Int64
	go func() {
		// Receive emitted events, this func will exit when test exits.
		for block := range logs {
			streamedLogs = append(streamedLogs, block.Logs...)
			streamedLogsCount.Add(int64(len(block.Logs)))
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

	// Wait for blocksWithLogsLength-followDistance blocks to be streamed.
Wait1:
	for {
		select {
		case <-ctx.Done():
			require.Failf(t, "timed out", "err: %v, streamedLogsCount: %d", ctx.Err(), streamedLogsCount.Load())
		case <-time.After(time.Millisecond * 5):
			if streamedLogsCount.Load() == int64(blocksWithLogsLength-followDistance) {
				break Wait1
			}
		}
	}

	// Create empty blocks with no transactions to advance the chain
	// followDistance blocks ahead.
	for i := 0; i < followDistance; i++ {
		sim.Commit()
		time.Sleep(delay)
	}
	// Wait for streamed logs to advance accordingly.
Wait2:
	for {
		select {
		case <-ctx.Done():
			require.Failf(t, "timed out", "err: %v, streamedLogsCount: %d", ctx.Err(), streamedLogsCount.Load())
		case <-time.After(time.Millisecond * 5):
			if streamedLogsCount.Load() == int64(blocksWithLogsLength) {
				break Wait2
			}
		}
	}
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

	rpcServer, _ := sim.Node().RPCHandler()
	httpsrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	defer rpcServer.Stop()
	defer httpsrv.Close()
	addr := httpToWebSocketURL(httpsrv.URL)

	// Deploy the contract
	parsed, _ := abi.JSON(strings.NewReader(callableAbi))
	auth, _ := bind.NewKeyedTransactorWithChainID(testKey, big.NewInt(1337))
	contractAddr, _, contract, err := bind.DeployContract(auth, parsed, ethcommon.FromHex(callableBin), sim.Client())
	if err != nil {
		t.Errorf("deploying contract: %v", err)
	}
	sim.Commit()

	client, err := New(ctx, addr, contractAddr, WithLogger(logger), WithLogBatchSize(2))
	require.NoError(t, err)

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
		case block := <-logChan:
			blockNumbers = append(blockNumbers, block.BlockNumber)
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
		for block := range logChan {
			blockNumbers = append(blockNumbers, block.BlockNumber)
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
	// TODO: fix reorg test
	// logger := zaptest.NewLogger(t)
	// const testTimeout = 2 * time.Second
	// ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	// defer cancel()

	// sim := simTestBackend(testAddr)

	// rpcServer, _ := sim.Node.RPCHandler()
	// httpsrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	// defer rpcServer.Stop()
	// defer httpsrv.Close()

	// addr := httpToWebSocketURL(httpsrv.URL)

	// // 1.
	// parsed, _ := abi.JSON(strings.NewReader(callableAbi))
	// auth, _ := bind.NewKeyedTransactorWithChainID(testKey, big.NewInt(1337))
	// contractAddr, _, contract, err := bind.DeployContract(auth, parsed, ethcommon.FromHex(callableBin), sim)
	// if err != nil {
	// 	t.Errorf("deploying contract: %v", err)
	// }
	// sim.Commit()

	// // Connect the client
	// const followDistance = 8
	// client, err := New(ctx, addr, contractAddr, WithLogger(logger), WithFollowDistance(followDistance))
	// require.NoError(t, err)

	// isReady, err := client.IsReady(ctx)
	// require.NoError(t, err)
	// require.True(t, isReady)
	// // 2.
	// logs := client.StreamLogs(ctx, 0)
	// // 3.
	// var parent *ethtypes.Header
	// var goodTransactions []ethcommon.Hash
	// // 4. Send some transactions
	// for i := 0; i < followDistance/2; i++ {
	// 	// Call contract to trigger event emit
	// 	tx, err := contract.Transact(auth, "Call")
	// 	if err != nil {
	// 		t.Errorf("transacting: %v", err)
	// 	}
	// 	sim.Commit()
	// 	if i == 0 {
	// 		goodTransactions = append(goodTransactions, tx.Hash())
	// 		parent = sim.Blockchain.CurrentBlock()
	// 	}
	// }
	// // 5. Fork off the chain after the first transaction
	// if err := sim.Fork(context.Background(), parent.Hash()); err != nil {
	// 	t.Errorf("forking: %v", err)
	// }
	// // 6. Add more blocks and 1 transaction after the fork
	// for i := 0; i < followDistance; i++ {
	// 	if i == 1 {
	// 		tx, err := contract.Transact(auth, "Call")
	// 		if err != nil {
	// 			t.Errorf("transacting: %v", err)
	// 		}
	// 		goodTransactions = append(goodTransactions, tx.Hash())
	// 	}
	// 	sim.Commit()
	// 	t.Log("committed block")
	// }
	// // 5.
	// for i, hash := range goodTransactions {
	// 	select {
	// 	case log := <-logs:
	// 		require.NotEmpty(t, log)
	// 		require.Equal(t, hash, log.TxHash)
	// 		t.Logf("got log from good transaction %d", i)
	// 	case <-ctx.Done():
	// 		t.Fatal("context canceled")
	// 	}
	// }
	// select {
	// case <-logs:
	// 	t.Fatal("should not receive log")
	// case <-ctx.Done():
	// }
	// if sim.Blockchain.CurrentBlock().Number.Uint64() != uint64(13) {
	// 	t.Error("wrong chain length")
	// }
	// require.NoError(t, client.Close())
	// require.NoError(t, sim.Close())
}

// TestSimSSV deploys the simplified SSVNetwork contract to generate events and receive at the client
func TestSimSSV(t *testing.T) {
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	const testTimeout = 1 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Create simulator instance
	sim := simTestBackend(testAddr)

	// Create JSON-RPC handler
	rpcServer, _ := sim.Node().RPCHandler()
	// Expose handler on a test server with ws open
	httpsrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	defer rpcServer.Stop()
	defer httpsrv.Close()
	addr := httpToWebSocketURL(httpsrv.URL)

	parsed, _ := abi.JSON(strings.NewReader(simcontract.SimcontractMetaData.ABI))
	auth, _ := bind.NewKeyedTransactorWithChainID(testKey, big.NewInt(1337))
	contractAddr, _, _, err := bind.DeployContract(auth, parsed, ethcommon.FromHex(simcontract.SimcontractMetaData.Bin), sim.Client())
	if err != nil {
		t.Errorf("deploying contract: %v", err)
	}
	sim.Commit()

	// Check contract code at the simulated blockchain
	contractCode, err := sim.Client().CodeAt(ctx, contractAddr, nil)
	if err != nil {
		t.Errorf("getting contract code: %v", err)
	}
	require.NotEmpty(t, contractCode)

	// Create a client and connect to the simulator
	client, err := New(ctx, addr, contractAddr, WithLogger(logger), WithFollowDistance(0))
	require.NoError(t, err)

	err = client.Healthy(ctx)
	require.NoError(t, err)

	logs := client.StreamLogs(ctx, 0)

	boundContract, err := simcontract.NewSimcontract(contractAddr, sim.Client())
	require.NoError(t, err)

	// Emit event OperatorAdded
	tx, err := boundContract.RegisterOperator(auth, ethcommon.Hex2Bytes("0xb24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"), big.NewInt(100_000_000))
	require.NoError(t, err)
	sim.Commit()
	receipt, err := sim.Client().TransactionReceipt(ctx, tx.Hash())
	if err != nil {
		t.Errorf("get receipt: %v", err)
	}
	require.Equal(t, uint64(0x1), receipt.Status)
	block := <-logs
	require.NotEmpty(t, block.Logs)
	require.Equal(t, ethcommon.HexToHash("0xd839f31c14bd632f424e307b36abff63ca33684f77f28e35dc13718ef338f7f4"), block.Logs[0].Topics[0])

	// Emit event OperatorRemoved
	tx, err = boundContract.RemoveOperator(auth, 1)
	require.NoError(t, err)
	sim.Commit()
	receipt, err = sim.Client().TransactionReceipt(ctx, tx.Hash())
	if err != nil {
		t.Errorf("get receipt: %v", err)
	}
	require.Equal(t, uint64(0x1), receipt.Status)
	block = <-logs
	require.NotEmpty(t, block.Logs)
	require.Equal(t, ethcommon.HexToHash("0x0e0ba6c2b04de36d6d509ec5bd155c43a9fe862f8052096dd54f3902a74cca3e"), block.Logs[0].Topics[0])

	// Emit event ValidatorAdded
	tx, err = boundContract.RegisterValidator(
		auth, ethcommon.Hex2Bytes("0x1"),
		[]uint64{1, 2, 3},
		ethcommon.Hex2Bytes("0x2"),
		big.NewInt(100_000_000),
		simcontract.CallableCluster{
			ValidatorCount:  3,
			NetworkFeeIndex: 1,
			Index:           1,
			Active:          true,
			Balance:         big.NewInt(100_000_000),
		})
	require.NoError(t, err)
	sim.Commit()
	receipt, err = sim.Client().TransactionReceipt(ctx, tx.Hash())
	if err != nil {
		t.Errorf("get receipt: %v", err)
	}
	require.Equal(t, uint64(0x1), receipt.Status)
	block = <-logs
	require.NotEmpty(t, block.Logs)
	require.Equal(t, ethcommon.HexToHash("0x48a3ea0796746043948f6341d17ff8200937b99262a0b48c2663b951ed7114e5"), block.Logs[0].Topics[0])

	// Emit event ValidatorRemoved
	tx, err = boundContract.RemoveValidator(
		auth,
		ethcommon.Hex2Bytes("0x1"),
		[]uint64{1, 2, 3},
		simcontract.CallableCluster{
			ValidatorCount:  3,
			NetworkFeeIndex: 1,
			Index:           1,
			Active:          true,
			Balance:         big.NewInt(100_000_000),
		})
	require.NoError(t, err)
	sim.Commit()
	receipt, err = sim.Client().TransactionReceipt(ctx, tx.Hash())
	if err != nil {
		t.Errorf("get receipt: %v", err)
	}
	require.Equal(t, uint64(0x1), receipt.Status)
	block = <-logs
	require.NotEmpty(t, block.Logs)
	require.Equal(t, ethcommon.HexToHash("0xccf4370403e5fbbde0cd3f13426479dcd8a5916b05db424b7a2c04978cf8ce6e"), block.Logs[0].Topics[0])

	// Emit event ClusterLiquidated
	tx, err = boundContract.Liquidate(
		auth,
		ethcommon.HexToAddress("0x1"),
		[]uint64{1, 2, 3},
		simcontract.CallableCluster{
			ValidatorCount:  3,
			NetworkFeeIndex: 1,
			Index:           1,
			Active:          true,
			Balance:         big.NewInt(100_000_000),
		})
	require.NoError(t, err)
	sim.Commit()
	receipt, err = sim.Client().TransactionReceipt(ctx, tx.Hash())
	if err != nil {
		t.Errorf("get receipt: %v", err)
	}
	require.Equal(t, uint64(0x1), receipt.Status)
	block = <-logs
	require.NotEmpty(t, block.Logs)
	require.Equal(t, ethcommon.HexToHash("0x1fce24c373e07f89214e9187598635036111dbb363e99f4ce498488cdc66e688"), block.Logs[0].Topics[0])

	// Emit event ClusterReactivated
	tx, err = boundContract.Reactivate(
		auth,
		[]uint64{1, 2, 3},
		big.NewInt(100_000_000),
		simcontract.CallableCluster{
			ValidatorCount:  3,
			NetworkFeeIndex: 1,
			Index:           1,
			Active:          true,
			Balance:         big.NewInt(100_000_000),
		})
	require.NoError(t, err)
	sim.Commit()
	receipt, err = sim.Client().TransactionReceipt(ctx, tx.Hash())
	if err != nil {
		t.Errorf("get receipt: %v", err)
	}
	require.Equal(t, uint64(0x1), receipt.Status)
	block = <-logs
	require.NotEmpty(t, block.Logs)
	require.Equal(t, ethcommon.HexToHash("0xc803f8c01343fcdaf32068f4c283951623ef2b3fa0c547551931356f456b6859"), block.Logs[0].Topics[0])

	// Emit event FeeRecipientAddressUpdated
	tx, err = boundContract.SetFeeRecipientAddress(
		auth,
		ethcommon.HexToAddress("0x1"),
	)
	require.NoError(t, err)
	sim.Commit()
	receipt, err = sim.Client().TransactionReceipt(ctx, tx.Hash())
	if err != nil {
		t.Errorf("get receipt: %v", err)
	}
	require.Equal(t, uint64(0x1), receipt.Status)
	block = <-logs
	require.NotEmpty(t, block.Logs)
	require.Equal(t, ethcommon.HexToHash("0x259235c230d57def1521657e7c7951d3b385e76193378bc87ef6b56bc2ec3548"), block.Logs[0].Topics[0])

	require.NoError(t, client.Close())
	require.NoError(t, sim.Close())
}

func TestSyncProgress(t *testing.T) {
	const testTimeout = 1 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Create simulator instance
	sim := simTestBackend(testAddr)

	// Create JSON-RPC handler
	rpcServer, _ := sim.Node().RPCHandler()
	// Expose handler on a test server with ws open
	httpsrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	defer rpcServer.Stop()
	defer httpsrv.Close()
	addr := httpToWebSocketURL(httpsrv.URL)

	parsed, _ := abi.JSON(strings.NewReader(simcontract.SimcontractMetaData.ABI))
	auth, _ := bind.NewKeyedTransactorWithChainID(testKey, big.NewInt(1337))
	contractAddr, _, _, err := bind.DeployContract(auth, parsed, ethcommon.FromHex(simcontract.SimcontractMetaData.Bin), sim.Client())
	if err != nil {
		t.Errorf("deploying contract: %v", err)
	}
	sim.Commit()

	// Check contract code at the simulated blockchain
	contractCode, err := sim.Client().CodeAt(ctx, contractAddr, nil)
	if err != nil {
		t.Errorf("getting contract code: %v", err)
	}
	require.NotEmpty(t, contractCode)

	// Create a client and connect to the simulator
	client, err := New(ctx, addr, contractAddr, WithHealthInvalidationInterval(0))
	require.NoError(t, err)

	err = client.Healthy(ctx)
	require.NoError(t, err)

	t.Run("out of sync", func(t *testing.T) {
		client.syncProgressFn = func(context.Context) (*ethereum.SyncProgress, error) {
			p := new(ethereum.SyncProgress)
			p.CurrentBlock = 5
			p.HighestBlock = 6
			return p, nil
		}
		err = client.Healthy(ctx)
		require.ErrorIs(t, err, errSyncing)
	})
	t.Run("within tolerable limits", func(t *testing.T) {
		client, err := New(ctx, addr, contractAddr, WithSyncDistanceTolerance(2))
		require.NoError(t, err)

		client.syncProgressFn = func(context.Context) (*ethereum.SyncProgress, error) {
			p := new(ethereum.SyncProgress)
			p.CurrentBlock = 5
			p.HighestBlock = 7
			return p, nil
		}
		err = client.Healthy(ctx)
		require.NoError(t, err)
	})
}

func httpToWebSocketURL(url string) string {
	return "ws:" + strings.TrimPrefix(url, "http:")
}
