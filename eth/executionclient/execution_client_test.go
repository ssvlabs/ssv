package executionclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
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

func simTestBackend(testAddr ethcommon.Address) *simulator.Backend {
	return simulator.NewBackend(
		ethtypes.GenesisAlloc{
			testAddr: {Balance: big.NewInt(10000000000000000)},
		},
		simulated.WithBlockGasLimit(10000000),
	)
}

// testEnv is a helper struct to set up and manage test environment.
type testEnv struct {
	ctx          context.Context
	t            *testing.T
	sim          *simulator.Backend
	rpcServer    *httptest.Server
	wsURL        string
	contractAddr ethcommon.Address
	auth         *bind.TransactOpts
	client       *ExecutionClient
}

// setupTestEnv creates a new test environment with simulators, contracts, and clients' setup.
func setupTestEnv(t *testing.T, testTimeout time.Duration) *testEnv {
	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	t.Cleanup(cancel)

	// Create simulator instance
	sim := simTestBackend(testAddr)
	t.Cleanup(func() { require.NoError(t, sim.Close()) })

	// Create JSON-RPC handler and setup WS server
	rpcServer, _ := sim.Node().RPCHandler()
	httpsrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	t.Cleanup(func() {
		rpcServer.Stop()
		httpsrv.Close()
	})
	wsURL := httpToWebSocketURL(httpsrv.URL)

	// Setup auth for transactions
	auth, _ := bind.NewKeyedTransactorWithChainID(testKey, big.NewInt(1337))

	return &testEnv{
		ctx:       ctx,
		t:         t,
		sim:       sim,
		rpcServer: httpsrv,
		wsURL:     wsURL,
		auth:      auth,
	}
}

// deployCallableContract deploys the test contract for event testing.
func (env *testEnv) deployCallableContract() (*bind.BoundContract, error) {
	parsed, _ := abi.JSON(strings.NewReader(callableAbi))
	contractAddr, _, contract, err := bind.DeployContract(
		env.auth,
		parsed,
		ethcommon.FromHex(callableBin),
		env.sim.Client(),
	)
	if err != nil {
		return nil, err
	}
	env.contractAddr = contractAddr
	env.sim.Commit()
	return contract, nil
}

// createClient creates and validates a new execution client with given options.
func (env *testEnv) createClient(options ...Option) error {
	return env.createClientWithCleanup(true, options...)
}

// createClientWithCleanup creates and initializes an execution client, optionally registering it for cleanup.
// If registerCleanup is false, the caller is responsible for closing the client.
func (env *testEnv) createClientWithCleanup(registerCleanup bool, options ...Option) error {
	allOptions := append([]Option{}, options...)
	var err error
	env.client, err = New(env.ctx, env.wsURL, env.contractAddr, allOptions...)
	if err != nil {
		return err
	}
	if registerCleanup {
		env.t.Cleanup(func() { require.NoError(env.t, env.client.Close()) })
	}

	return env.client.Healthy(env.ctx)
}

// createBlocksWithLogs creates a specified number of blocks with Call transactions.
func (env *testEnv) createBlocksWithLogs(contract *bind.BoundContract, count int, delay time.Duration) error {
	for i := 0; i < count; i++ {
		_, err := contract.Transact(env.auth, "Call")
		if err != nil {
			return err
		}
		env.sim.Commit()
		if delay > 0 {
			time.Sleep(delay)
		}
	}
	return nil
}

// TestFetchHistoricalLogs tests the FetchHistoricalLogs function of the client.
func TestFetchHistoricalLogs(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("successfully fetches historical logs within follow distance", func(t *testing.T) {
		env := setupTestEnv(t, 1*time.Second)
		contract, err := env.deployCallableContract()
		require.NoError(t, err)

		// Create a client and connect to the simulator
		const followDistance = 8
		err = env.createClient(
			WithLogger(logger),
			WithFollowDistance(followDistance),
			WithConnectionTimeout(2*time.Second),
			WithReconnectionInitialInterval(2*time.Second),
		)
		require.NoError(t, err)

		// Create blocks with transactions
		err = env.createBlocksWithLogs(contract, blocksWithLogsLength, 0)
		require.NoError(t, err)

		// Fetch all logs history starting from block 0
		var fetchedLogs []ethtypes.Log
		logs, fetchErrCh, err := env.client.FetchHistoricalLogs(env.ctx, 0)
		require.NoError(t, err)

		for block := range logs {
			fetchedLogs = append(fetchedLogs, block.Logs...)
		}
		require.NotEmpty(t, fetchedLogs)

		expectedSeenLogs := blocksWithLogsLength - followDistance
		require.Equal(t, expectedSeenLogs, len(fetchedLogs))

		select {
		case err := <-fetchErrCh:
			require.NoError(t, err)
		case <-env.ctx.Done():
			require.Fail(t, "timeout")
		}
	})

	t.Run("error when currentBlock < followDistance", func(t *testing.T) {
		env := setupTestEnv(t, 1*time.Second)
		_, err := env.deployCallableContract()
		require.NoError(t, err)

		// Create a client with a large followDistance
		const followDistance = 100 // Much larger than the current block number
		err = env.createClient(
			WithLogger(logger),
			WithFollowDistance(followDistance),
			WithConnectionTimeout(2*time.Second),
			WithReconnectionInitialInterval(2*time.Second),
		)
		require.NoError(t, err)

		// Fetch logs - should fail because the currentBlock < followDistance
		logs, fetchErrCh, err := env.client.FetchHistoricalLogs(env.ctx, 0)
		require.ErrorIs(t, err, ErrNothingToSync)
		require.Nil(t, logs)
		require.Nil(t, fetchErrCh)
	})

	t.Run("error when toBlock < fromBlock", func(t *testing.T) {
		env := setupTestEnv(t, 1*time.Second)
		contract, err := env.deployCallableContract()
		require.NoError(t, err)

		// Create a client
		const followDistance = 8
		err = env.createClient(
			WithLogger(logger),
			WithFollowDistance(followDistance),
			WithConnectionTimeout(2*time.Second),
			WithReconnectionInitialInterval(2*time.Second),
		)
		require.NoError(t, err)

		// Create some blocks
		err = env.createBlocksWithLogs(contract, 10, 0)
		require.NoError(t, err)

		// Fetch logs with fromBlock > toBlock
		currentBlock, err := env.client.client.BlockNumber(env.ctx)
		require.NoError(t, err)

		// Set fromBlock to a value greater than the currentBlock - followDistance
		fromBlock := currentBlock - followDistance + 10

		logs, fetchErrCh, err := env.client.FetchHistoricalLogs(env.ctx, fromBlock)
		require.ErrorIs(t, err, ErrNothingToSync)
		require.Nil(t, logs)
		require.Nil(t, fetchErrCh)
	})

	t.Run("error when BlockNumber fails", func(t *testing.T) {
		env := setupTestEnv(t, 1*time.Second)
		_, err := env.deployCallableContract()
		require.NoError(t, err)

		// Create a client - connection should succeed initially
		err = env.createClient(
			WithLogger(logger),
			WithFollowDistance(8),
			WithConnectionTimeout(100*time.Millisecond),
			WithReconnectionInitialInterval(100*time.Millisecond),
		)
		require.NoError(t, err) // Connection is established initially

		// Create a context with a very short timeout to ensure BlockNumber fails
		blockNumCtx, blockNumCancel := context.WithTimeout(env.ctx, 1*time.Nanosecond)
		defer blockNumCancel()

		// Fetch logs - should fail because BlockNumber returns an error
		logs, fetchErrCh, err := env.client.FetchHistoricalLogs(blockNumCtx, 0)
		require.Error(t, err)
		require.Nil(t, logs)
		require.Nil(t, fetchErrCh)
		require.ErrorContains(t, err, "failed to get current block")
	})
}

// TestFetchHistoricalLogs_Subdivide tests handling of EIP-1474 query limits.
// When receiving error code -32005 ("Limit exceeded") from eth_getLogs requests,
// the client recursively subdivides the block range until successful or until
// hitting a non-recoverable error.
func TestFetchHistoricalLogs_Subdivide(t *testing.T) {
	testCases := []struct {
		name        string
		totalBlocks int
		threshold   uint64 // block range that won't be subdivided
		wantLogs    int
		wantCalls   int32
		httpError   bool // if true, simulate an HTTP error
	}{
		// 1. Call for blocks 0-1 (succeeds) [count: 1]
		// eth_getLogs calls: 1
		{"single log", 1, 5, 1, 1, false},

		// 1. Call for blocks 0-2 (succeeds) [count: 1]
		// eth_getLogs calls: 1
		{"multiple logs", 2, 5, 2, 1, false},

		// 1. Call for blocks 0-4 (query limited) [count: 1]
		// 2. Subdivide into 0-2 and 3-4
		// 3. Call for blocks 3-4 (succeeds) [count: 2]
		// 4. Call for blocks 0-2 (succeeds) [count: 3]
		// eth_getLogs calls: 3
		{"full subdivision", 4, 2, 4, 3, false},

		// 1. Call for blocks 0-9 (query limited) [count: 1]
		// 2. Subdivide into 0-4 and 5-9
		// 3. Call for blocks 5-9 (succeeds) [count: 2] - Range of 5 blocks, threshold is 4
		// 4. Call for blocks 0-4 (query limited) [count: 3] - Range of 5 blocks, threshold is 3
		// 5. Subdivide 0-4 into 0-2 and 3-4
		// 6. Call for blocks 0-2 (succeeds) [count: 4] - Range of 3 blocks
		// 7. Call for blocks 3-4 (succeeds) [count: 5] - Range of 2 blocks
		// eth_getLogs calls: 5
		{"partial subdivision", 9, 4, 9, 5, false},

		// Test for non-RPC error (HTTP 500)
		// 1. Call for blocks 0-4 (returns HTTP 500) [count: 1]
		// function should exit with the error, not try to subdivide
		// eth_getLogs calls: 1
		{"http error", 4, 1, 0, 1, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			env := setupTestEnv(t, 5*time.Second)
			contract, err := env.deployCallableContract()
			require.NoError(t, err)

			// mine some blocks
			for i := 0; i < tc.totalBlocks; i++ {
				_, err := contract.Transact(env.auth, "Call")
				require.NoError(t, err)
				env.sim.Commit()
			}

			// custom eth_getLogs with call count
			rpcSrv, _ := env.sim.Node().RPCHandler()
			base := http.Handler(rpcSrv)
			var callCount atomic.Int32

			wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				var req map[string]interface{}
				_ = json.Unmarshal(raw, &req)

				if req["method"] == "eth_getLogs" {
					callCount.Add(1)

					if tc.httpError {
						http.Error(w, "Internal Server Error", http.StatusInternalServerError)
						return
					}

					flt := req["params"].([]interface{})[0].(map[string]interface{})
					from, _ := strconv.ParseInt(strings.TrimPrefix(flt["fromBlock"].(string), "0x"), 16, 64)
					to, _ := strconv.ParseInt(strings.TrimPrefix(flt["toBlock"].(string), "0x"), 16, 64)
					if uint64(to-from) > tc.threshold {
						w.Header().Set("Content-Type", "application/json")
						_ = json.NewEncoder(w).Encode(map[string]interface{}{
							"jsonrpc": "2.0",
							"id":      req["id"],
							"error": map[string]interface{}{
								"code":    -32005,
								"message": "query limit exceeded",
							},
						})
						return
					}
				}
				r.Body = io.NopCloser(bytes.NewReader(raw))
				base.ServeHTTP(w, r)
			})
			srv := httptest.NewServer(wrapped)
			t.Cleanup(srv.Close)

			opts := []Option{WithFollowDistance(0), WithLogBatchSize(100000)}

			client, err := New(t.Context(),
				srv.URL,
				env.contractAddr,
				opts...,
			)
			require.NoError(t, err)

			t.Cleanup(func() { require.NoError(t, client.Close()) })

			logsCh, errCh, err := client.FetchHistoricalLogs(t.Context(), 0)
			require.NoError(t, err)

			var all []ethtypes.Log
			for blk := range logsCh {
				all = append(all, blk.Logs...)
			}

			err = <-errCh
			if tc.httpError {
				require.Error(t, err)
				require.Equal(t, tc.wantCalls, callCount.Load())

				return
			}

			require.NoError(t, err)
			require.Len(t, all, tc.wantLogs)
			require.Equal(t, tc.wantCalls, callCount.Load())
		})
	}
}

func TestStreamLogs(t *testing.T) {
	t.Run("successfully streams logs", func(t *testing.T) {
		logger, err := zap.NewDevelopment()
		require.NoError(t, err)

		env := setupTestEnv(t, 2*time.Second)

		// Deploy the contract
		contract, err := env.deployCallableContract()
		require.NoError(t, err)

		// Create a client and connect to the simulator
		const followDistance = 2
		err = env.createClient(WithLogger(logger), WithFollowDistance(followDistance))
		require.NoError(t, err)

		logs := env.client.StreamLogs(env.ctx, 0)
		var streamedLogs []ethtypes.Log
		var streamedLogsCount atomic.Int64
		go func() {
			// Receive emitted events, this func will exit when the test exits.
			for block := range logs {
				streamedLogs = append(streamedLogs, block.Logs...)
				streamedLogsCount.Add(int64(len(block.Logs)))
			}
		}()

		// Create blocks with transactions
		delay := time.Millisecond * 10
		err = env.createBlocksWithLogs(contract, blocksWithLogsLength, delay)
		require.NoError(t, err)

		// Wait for blocksWithLogsLength-followDistance blocks to be streamed.
	Wait1:
		for {
			select {
			case <-env.ctx.Done():
				require.Failf(t, "timed out", "err: %v, streamedLogsCount: %d", env.ctx.Err(), streamedLogsCount.Load())
			case <-time.After(time.Millisecond * 5):
				if streamedLogsCount.Load() == int64(blocksWithLogsLength-followDistance) {
					break Wait1
				}
			}
		}

		// Create empty blocks with no transactions to advance the chain
		// followDistance blocks ahead.
		for i := 0; i < followDistance; i++ {
			env.sim.Commit()
			time.Sleep(delay)
		}
		// Wait for streamed logs to advance accordingly.
	Wait2:
		for {
			select {
			case <-env.ctx.Done():
				require.Failf(t, "timed out", "err: %v, streamedLogsCount: %d", env.ctx.Err(), streamedLogsCount.Load())
			case <-time.After(time.Millisecond * 5):
				if streamedLogsCount.Load() == int64(blocksWithLogsLength) {
					break Wait2
				}
			}
		}
		require.NotEmpty(t, streamedLogs)
		require.Equal(t, blocksWithLogsLength, len(streamedLogs))
	})

	t.Run("returns when context is canceled", func(t *testing.T) {
		logger, err := zap.NewDevelopment()
		require.NoError(t, err)

		env := setupTestEnv(t, 2*time.Second)

		// Deploy the contract
		_, err = env.deployCallableContract()
		require.NoError(t, err)

		// Create a client and connect to the simulator
		err = env.createClient(WithLogger(logger))
		require.NoError(t, err)

		// Create a context that can be canceled
		ctx, cancel := context.WithCancel(env.ctx)
		defer cancel()

		// Start streaming logs
		logs := env.client.StreamLogs(ctx, 0)

		// Set up a channel to detect when the log channel is closed
		done := make(chan struct{})
		go func() {
			// This goroutine should exit when the log channel is closed
			for range logs {
				// Just consume logs
			}
			close(done)
		}()

		// Cancel the context to trigger the first return case
		cancel()

		// Wait for the log channel to be closed
		select {
		case <-done:
			// Success - the log channel was closed
		case <-time.After(1 * time.Second):
			require.Fail(t, "StreamLogs did not return when context was canceled")
		}
	})

	t.Run("returns when client is closed", func(t *testing.T) {
		logger, err := zap.NewDevelopment()
		require.NoError(t, err)

		env := setupTestEnv(t, 2*time.Second)

		// Deploy the contract
		_, err = env.deployCallableContract()
		require.NoError(t, err)

		// Create a client and connect to the simulator
		// Don't register cleanup since we'll explicitly close the client in this test
		err = env.createClientWithCleanup(false, WithLogger(logger))
		require.NoError(t, err)

		// Start streaming logs
		logs := env.client.StreamLogs(env.ctx, 0)

		// Set up a channel to detect when the log channel is closed
		done := make(chan struct{})
		go func() {
			// This goroutine should exit when the log channel is closed
			for range logs {
				// Just consume logs
			}
			close(done)
		}()

		// Close the client to trigger the second return case
		require.NoError(t, env.client.Close())

		// Wait for the log channel to be closed
		select {
		case <-done:
			// Success - the log channel was closed
		case <-time.After(1 * time.Second):
			require.Fail(t, "StreamLogs did not return when client was closed")
		}
	})
}

// TestFetchLogsInBatches tests the fetchLogsInBatches function of the client.
func TestFetchLogsInBatches(t *testing.T) {
	logger := zaptest.NewLogger(t)
	env := setupTestEnv(t, 1*time.Second)

	// Deploy the contract
	contract, err := env.deployCallableContract()
	require.NoError(t, err)

	err = env.createClient(WithLogger(logger), WithLogBatchSize(2))
	require.NoError(t, err)

	// Create blocks with transactions
	err = env.createBlocksWithLogs(contract, blocksWithLogsLength, 0)
	require.NoError(t, err)

	t.Run("startBlock is greater than endBlock", func(t *testing.T) {
		logChan, errChan := env.client.fetchLogsInBatches(env.ctx, 10, 5)
		select {
		case <-logChan:
			require.Fail(t, "Should not receive log when startBlock > endBlock")
		case err := <-errChan:
			require.ErrorIs(t, err, ErrBadInput)
		case <-env.ctx.Done():
			require.Fail(t, "fetchLogsInBatches did not return in time when startBlock > endBlock")
		}
	})

	t.Run("startBlock is same as endBlock", func(t *testing.T) {
		var blockNumbers []uint64

		logChan, errChan := env.client.fetchLogsInBatches(env.ctx, 5, 5)
		select {
		case block := <-logChan:
			blockNumbers = append(blockNumbers, block.BlockNumber)
		case err := <-errChan:
			t.Fatalf("fetchLogsInBatches failed: %v", err)
		case <-env.ctx.Done():
			require.Fail(t, "fetchLogsInBatches did not return in time when fromBlock == toBlock")
		}

		require.Equal(t, []uint64{5}, blockNumbers)
	})

	t.Run("startBlock is less than endBlock", func(t *testing.T) {
		var blockNumbers []uint64

		logChan, errChan := env.client.fetchLogsInBatches(env.ctx, 3, 11)
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
		canceledCtx, cancel := context.WithCancel(env.ctx)
		cancel()

		logChan, errChan := env.client.fetchLogsInBatches(canceledCtx, 0, 5)
		select {
		case <-logChan:
			require.Fail(t, "Should not receive log when context is canceled")
		case err := <-errChan:
			require.Error(t, err, "fetchLogsInBatches should return an error when context is canceled")
		case <-canceledCtx.Done():
		}
	})
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
	// ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	// defer cancel()

	// sim := simTestBackend(testAddr)

	// rpcServer, _ := sim.Node.RPCHandler()
	// httpsrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	// defer rpcServer.Stop()
	// defer httpsrv.Close()

	// httpAddr := httpToWebSocketURL(httpsrv.URL)

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
	// client, err := New(ctx, httpAddr, contractAddr, WithLogger(logger), WithFollowDistance(followDistance))
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
	// if err := sim.Fork(t.Context(), parent.Hash()); err != nil {
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

// deploySimContract deploys the SSV simulator contract.
func (env *testEnv) deploySimContract() (*simcontract.Simcontract, error) {
	parsed, _ := abi.JSON(strings.NewReader(simcontract.SimcontractMetaData.ABI))
	contractAddr, _, _, err := bind.DeployContract(
		env.auth,
		parsed,
		ethcommon.FromHex(simcontract.SimcontractMetaData.Bin),
		env.sim.Client(),
	)
	if err != nil {
		return nil, err
	}
	env.contractAddr = contractAddr
	env.sim.Commit()

	// Verify contract code exists
	contractCode, err := env.sim.Client().CodeAt(env.ctx, contractAddr, nil)
	if err != nil {
		return nil, err
	}
	if len(contractCode) == 0 {
		return nil, fmt.Errorf("empty contract code")
	}

	// Return the bound contract
	return simcontract.NewSimcontract(contractAddr, env.sim.Client())
}

// TestSimSSV deploys the simplified SSVNetwork contract to generate events and receive at the client.
func TestSimSSV(t *testing.T) {
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)

	env := setupTestEnv(t, 1*time.Second)

	// Deploy the SSV contract
	boundContract, err := env.deploySimContract()
	require.NoError(t, err)

	// Create a client and connect to the simulator
	err = env.createClient(WithLogger(logger), WithFollowDistance(0))
	require.NoError(t, err)

	logs := env.client.StreamLogs(env.ctx, 0)

	// Emit event OperatorAdded
	tx, err := boundContract.RegisterOperator(env.auth, ethcommon.Hex2Bytes("0xb24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"), big.NewInt(100_000_000))
	require.NoError(t, err)
	env.sim.Commit()
	receipt, err := env.sim.Client().TransactionReceipt(env.ctx, tx.Hash())
	if err != nil {
		t.Errorf("get receipt: %v", err)
	}
	require.Equal(t, uint64(0x1), receipt.Status)
	block := <-logs
	require.NotEmpty(t, block.Logs)
	require.Equal(t, ethcommon.HexToHash("0xd839f31c14bd632f424e307b36abff63ca33684f77f28e35dc13718ef338f7f4"), block.Logs[0].Topics[0])

	// Emit event OperatorRemoved
	tx, err = boundContract.RemoveOperator(env.auth, 1)
	require.NoError(t, err)
	env.sim.Commit()
	receipt, err = env.sim.Client().TransactionReceipt(env.ctx, tx.Hash())
	if err != nil {
		t.Errorf("get receipt: %v", err)
	}
	require.Equal(t, uint64(0x1), receipt.Status)
	block = <-logs
	require.NotEmpty(t, block.Logs)
	require.Equal(t, ethcommon.HexToHash("0x0e0ba6c2b04de36d6d509ec5bd155c43a9fe862f8052096dd54f3902a74cca3e"), block.Logs[0].Topics[0])

	// Emit event ValidatorAdded
	tx, err = boundContract.RegisterValidator(
		env.auth, ethcommon.Hex2Bytes("0x1"),
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
	env.sim.Commit()
	receipt, err = env.sim.Client().TransactionReceipt(env.ctx, tx.Hash())
	if err != nil {
		t.Errorf("get receipt: %v", err)
	}
	require.Equal(t, uint64(0x1), receipt.Status)
	block = <-logs
	require.NotEmpty(t, block.Logs)
	require.Equal(t, ethcommon.HexToHash("0x48a3ea0796746043948f6341d17ff8200937b99262a0b48c2663b951ed7114e5"), block.Logs[0].Topics[0])

	// Emit event ValidatorRemoved
	tx, err = boundContract.RemoveValidator(
		env.auth,
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
	env.sim.Commit()
	receipt, err = env.sim.Client().TransactionReceipt(env.ctx, tx.Hash())
	if err != nil {
		t.Errorf("get receipt: %v", err)
	}
	require.Equal(t, uint64(0x1), receipt.Status)
	block = <-logs
	require.NotEmpty(t, block.Logs)
	require.Equal(t, ethcommon.HexToHash("0xccf4370403e5fbbde0cd3f13426479dcd8a5916b05db424b7a2c04978cf8ce6e"), block.Logs[0].Topics[0])

	// Emit event ClusterLiquidated
	tx, err = boundContract.Liquidate(
		env.auth,
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
	env.sim.Commit()
	receipt, err = env.sim.Client().TransactionReceipt(env.ctx, tx.Hash())
	if err != nil {
		t.Errorf("get receipt: %v", err)
	}
	require.Equal(t, uint64(0x1), receipt.Status)
	block = <-logs
	require.NotEmpty(t, block.Logs)
	require.Equal(t, ethcommon.HexToHash("0x1fce24c373e07f89214e9187598635036111dbb363e99f4ce498488cdc66e688"), block.Logs[0].Topics[0])

	// Emit event ClusterReactivated
	tx, err = boundContract.Reactivate(
		env.auth,
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
	env.sim.Commit()
	receipt, err = env.sim.Client().TransactionReceipt(env.ctx, tx.Hash())
	if err != nil {
		t.Errorf("get receipt: %v", err)
	}
	require.Equal(t, uint64(0x1), receipt.Status)
	block = <-logs
	require.NotEmpty(t, block.Logs)
	require.Equal(t, ethcommon.HexToHash("0xc803f8c01343fcdaf32068f4c283951623ef2b3fa0c547551931356f456b6859"), block.Logs[0].Topics[0])

	// Emit event FeeRecipientAddressUpdated
	tx, err = boundContract.SetFeeRecipientAddress(
		env.auth,
		ethcommon.HexToAddress("0x1"),
	)
	require.NoError(t, err)
	env.sim.Commit()
	receipt, err = env.sim.Client().TransactionReceipt(env.ctx, tx.Hash())
	if err != nil {
		t.Errorf("get receipt: %v", err)
	}
	require.Equal(t, uint64(0x1), receipt.Status)
	block = <-logs
	require.NotEmpty(t, block.Logs)
	require.Equal(t, ethcommon.HexToHash("0x259235c230d57def1521657e7c7951d3b385e76193378bc87ef6b56bc2ec3548"), block.Logs[0].Topics[0])
}

// TestFilterLogs tests the FilterLogs method of the client.
func TestFilterLogs(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("successfully filters logs", func(t *testing.T) {
		env := setupTestEnv(t, 1*time.Second)

		// Deploy the contract
		contract, err := env.deployCallableContract()
		require.NoError(t, err)

		// Create a client and connect to the simulator
		err = env.createClient(WithLogger(logger))
		require.NoError(t, err)

		// Create blocks with transactions
		err = env.createBlocksWithLogs(contract, 5, 0)
		require.NoError(t, err)

		// Test the FilterLogs method
		logs, err := env.client.FilterLogs(env.ctx, ethereum.FilterQuery{
			Addresses: []ethcommon.Address{env.contractAddr},
			FromBlock: big.NewInt(0),
			ToBlock:   big.NewInt(6),
		})
		require.NoError(t, err)
		require.NotEmpty(t, logs)
		require.Equal(t, 5, len(logs))

		// Verify log details
		for _, log := range logs {
			require.Equal(t, env.contractAddr, log.Address)
			require.Equal(t, ethcommon.HexToHash("0x81fab7a4a0aa961db47eefc81f143a5220e8c8495260dd65b1356f1d19d3c7b8"), log.Topics[0])
		}
	})

	t.Run("error when FilterLogs fails", func(t *testing.T) {
		env := setupTestEnv(t, 1*time.Second)
		_, err := env.deployCallableContract()
		require.NoError(t, err)

		// Create a client - connection should succeed initially
		err = env.createClient(
			WithLogger(logger),
			WithConnectionTimeout(100*time.Millisecond),
		)
		require.NoError(t, err) // Connection is established initially

		// Create a context with a very short timeout to ensure FilterLogs fails
		timeoutCtx, cancel := context.WithTimeout(env.ctx, 1*time.Nanosecond)
		defer cancel()

		// FilterLogs should fail because of the short timeout
		logs, err := env.client.FilterLogs(timeoutCtx, ethereum.FilterQuery{
			Addresses: []ethcommon.Address{env.contractAddr},
			FromBlock: big.NewInt(0),
			ToBlock:   big.NewInt(1),
		})
		require.Error(t, err)
		require.Empty(t, logs)
	})
}

// TestSubscribeFilterLogs tests the SubscribeFilterLogs method of the client.
func TestSubscribeFilterLogs(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("successfully subscribes to filter logs", func(t *testing.T) {
		env := setupTestEnv(t, 2*time.Second)

		// Deploy the contract
		contract, err := env.deployCallableContract()
		require.NoError(t, err)

		// Create a client and connect to the simulator
		err = env.createClient(WithLogger(logger))
		require.NoError(t, err)

		// Set up a channel to receive logs
		logCh := make(chan ethtypes.Log)

		// Subscribe to filter logs
		query := ethereum.FilterQuery{
			Addresses: []ethcommon.Address{env.contractAddr},
		}
		sub, err := env.client.SubscribeFilterLogs(env.ctx, query, logCh)
		require.NoError(t, err)
		require.NotNil(t, sub)

		// Create a goroutine to collect logs
		var receivedLogs []ethtypes.Log
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 3; i++ {
				select {
				case log := <-logCh:
					receivedLogs = append(receivedLogs, log)
				case err := <-sub.Err():
					require.NoError(t, err)
					return
				case <-env.ctx.Done():
					return
				}
			}
		}()

		// Create blocks with transactions
		err = env.createBlocksWithLogs(contract, 3, 10*time.Millisecond)
		require.NoError(t, err)

		// Wait for logs to be received
		wg.Wait()

		// Verify logs were received
		require.Equal(t, 3, len(receivedLogs))
		for _, log := range receivedLogs {
			require.Equal(t, env.contractAddr, log.Address)
			require.Equal(t, ethcommon.HexToHash("0x81fab7a4a0aa961db47eefc81f143a5220e8c8495260dd65b1356f1d19d3c7b8"), log.Topics[0])
		}

		// Unsubscribe
		sub.Unsubscribe()
	})

	t.Run("error when SubscribeFilterLogs fails", func(t *testing.T) {
		env := setupTestEnv(t, 1*time.Second)
		_, err := env.deployCallableContract()
		require.NoError(t, err)

		// Create a client - connection should succeed initially
		err = env.createClient(
			WithLogger(logger),
			WithConnectionTimeout(100*time.Millisecond),
		)
		require.NoError(t, err) // Connection is established initially

		// Create a context with a very short timeout to ensure SubscribeFilterLogs fails
		timeoutCtx, cancel := context.WithTimeout(env.ctx, 1*time.Nanosecond)
		defer cancel()

		// Set up a channel to receive logs
		logCh := make(chan ethtypes.Log)

		// Subscribe to filter logs - should fail because of the short timeout
		query := ethereum.FilterQuery{
			Addresses: []ethcommon.Address{env.contractAddr},
		}
		sub, err := env.client.SubscribeFilterLogs(timeoutCtx, query, logCh)
		require.Error(t, err)
		require.Nil(t, sub)
	})
}

// TestBlockByNumber tests the BlockByNumber method of the client.
func TestBlockByNumber(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("successfully gets block by number", func(t *testing.T) {
		env := setupTestEnv(t, 1*time.Second)

		// Deploy the contract
		_, err := env.deployCallableContract()
		require.NoError(t, err)

		// Create a client and connect to the simulator
		err = env.createClient(WithLogger(logger))
		require.NoError(t, err)

		// Create some blocks
		for i := 0; i < 5; i++ {
			env.sim.Commit()
		}

		// Test the BlockByNumber method with specific block number
		block, err := env.client.BlockByNumber(env.ctx, big.NewInt(2))
		require.NoError(t, err)
		require.NotNil(t, block)
		require.Equal(t, uint64(2), block.NumberU64())

		// Test the BlockByNumber method with nil (latest block)
		latestBlock, err := env.client.BlockByNumber(env.ctx, nil)
		require.NoError(t, err)
		require.NotNil(t, latestBlock)
		require.Equal(t, uint64(6), latestBlock.NumberU64()) // Genesis + 1 from deploy + 5 from loop
	})

	t.Run("error when BlockByNumber fails", func(t *testing.T) {
		env := setupTestEnv(t, 1*time.Second)
		_, err := env.deployCallableContract()
		require.NoError(t, err)

		// Create a client - connection should succeed initially
		err = env.createClient(
			WithLogger(logger),
			WithConnectionTimeout(100*time.Millisecond),
		)
		require.NoError(t, err) // Connection is established initially

		// Create a context with a very short timeout to ensure BlockByNumber fails
		timeoutCtx, cancel := context.WithTimeout(env.ctx, 1*time.Nanosecond)
		defer cancel()

		// BlockByNumber should fail because of the short timeout
		block, err := env.client.BlockByNumber(timeoutCtx, big.NewInt(1))
		require.Error(t, err)
		require.Nil(t, block)
	})
}

// TestHeaderByNumber tests the HeaderByNumber method of the client.
func TestHeaderByNumber(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("successfully gets header by number", func(t *testing.T) {
		env := setupTestEnv(t, 1*time.Second)

		// Deploy the contract
		_, err := env.deployCallableContract()
		require.NoError(t, err)

		// Create a client and connect to the simulator
		err = env.createClient(WithLogger(logger))
		require.NoError(t, err)

		// Create some blocks
		for i := 0; i < 5; i++ {
			env.sim.Commit()
		}

		// Test the HeaderByNumber method with specific block number
		header, err := env.client.HeaderByNumber(env.ctx, big.NewInt(2))
		require.NoError(t, err)
		require.NotNil(t, header)
		require.Equal(t, uint64(2), header.Number.Uint64())

		// Test the HeaderByNumber method with nil (latest block)
		latestHeader, err := env.client.HeaderByNumber(env.ctx, nil)
		require.NoError(t, err)
		require.NotNil(t, latestHeader)
		require.Equal(t, uint64(6), latestHeader.Number.Uint64()) // Genesis + 1 from deploy + 5 from loop
	})

	t.Run("error when HeaderByNumber fails", func(t *testing.T) {
		env := setupTestEnv(t, 1*time.Second)
		_, err := env.deployCallableContract()
		require.NoError(t, err)

		// Create a client - connection should succeed initially
		err = env.createClient(
			WithLogger(logger),
			WithConnectionTimeout(100*time.Millisecond),
		)
		require.NoError(t, err) // Connection is established initially

		// Create a context with a very short timeout to ensure HeaderByNumber fails
		timeoutCtx, cancel := context.WithTimeout(env.ctx, 1*time.Nanosecond)
		defer cancel()

		// HeaderByNumber should fail because of the short timeout
		header, err := env.client.HeaderByNumber(timeoutCtx, big.NewInt(1))
		require.Error(t, err)
		require.Nil(t, header)
	})
}

// TestFilterer tests the Filterer method of the client.
func TestFilterer(t *testing.T) {
	logger := zaptest.NewLogger(t)
	env := setupTestEnv(t, 1*time.Second)

	// Deploy the contract
	_, err := env.deployCallableContract()
	require.NoError(t, err)

	// Create a client and connect to the simulator
	err = env.createClient(WithLogger(logger))
	require.NoError(t, err)

	// Test the Filterer method
	filterer, err := env.client.Filterer()
	require.NoError(t, err)
	require.NotNil(t, filterer)
}

// TestSyncProgress tests the sync progress of the client.
func TestSyncProgress(t *testing.T) {
	env := setupTestEnv(t, 1*time.Second)

	// Deploy the contract
	_, err := env.deploySimContract()
	require.NoError(t, err)

	// Create a client and connect to the simulator
	err = env.createClient(WithHealthInvalidationInterval(0))
	require.NoError(t, err)

	err = env.client.Healthy(env.ctx)
	require.NoError(t, err)

	t.Run("out of sync", func(t *testing.T) {
		env.client.syncProgressFn = func(context.Context) (*ethereum.SyncProgress, error) {
			p := new(ethereum.SyncProgress)
			p.CurrentBlock = 5
			p.HighestBlock = 6
			return p, nil
		}
		err = env.client.Healthy(env.ctx)
		require.ErrorIs(t, err, errSyncing)
	})

	t.Run("within tolerable limits", func(t *testing.T) {
		client, err := New(
			env.ctx,
			env.wsURL,
			env.contractAddr,
			WithSyncDistanceTolerance(2),
		)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, client.Close()) })

		client.syncProgressFn = func(context.Context) (*ethereum.SyncProgress, error) {
			p := new(ethereum.SyncProgress)
			p.CurrentBlock = 5
			p.HighestBlock = 7
			return p, nil
		}
		err = client.Healthy(env.ctx)
		require.NoError(t, err)
	})
}

// TestHealthy tests the Healthy method of the client.
func TestHealthy(t *testing.T) {
	t.Run("returns ErrClosed when client is closed", func(t *testing.T) {
		env := setupTestEnv(t, 1*time.Second)
		_, err := env.deploySimContract()
		require.NoError(t, err)

		// Create a client and connect to the simulator
		err = env.createClientWithCleanup(false)
		require.NoError(t, err)

		// Close the client using our safe method
		require.NoError(t, env.client.Close())

		// Healthy should return ErrClosed
		err = env.client.Healthy(env.ctx)
		require.ErrorIs(t, err, ErrClosed)
	})

	t.Run("returns nil when health check was recently performed", func(t *testing.T) {
		env := setupTestEnv(t, 1*time.Second)
		_, err := env.deploySimContract()
		require.NoError(t, err)

		// Create a client with a health invalidation interval
		err = env.createClient(WithHealthInvalidationInterval(10 * time.Second))
		require.NoError(t, err)

		// First call to Healthy should perform the actual health check
		err = env.client.Healthy(env.ctx)
		require.NoError(t, err)

		// Mock the syncProgressFn to return an error, to verify it's not called
		env.client.syncProgressFn = func(context.Context) (*ethereum.SyncProgress, error) {
			return nil, errors.New("this should not be called")
		}

		// Second call to Healthy should return nil without performing the health check
		err = env.client.Healthy(env.ctx)
		require.NoError(t, err)
	})
}

// httpToWebSocketURL converts an HTTP URL to a WebSocket URL.
func httpToWebSocketURL(url string) string {
	return "ws:" + strings.TrimPrefix(url, "http:")
}
