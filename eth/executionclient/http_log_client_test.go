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
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/ssvlabs/ssv/eth/simulator"
)

// httpLogClientTestEnv provides test environment for HTTP log client testing.
type httpLogClientTestEnv struct {
	ctx          context.Context
	t            *testing.T
	sim          *simulator.Backend
	httpServer   *httptest.Server
	contractAddr ethcommon.Address
	auth         *bind.TransactOpts
}

// setupHTTPLogClientTestEnv creates a new test environment for HTTP log client testing.
func setupHTTPLogClientTestEnv(t *testing.T, testTimeout time.Duration) *httpLogClientTestEnv {
	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	t.Cleanup(cancel)

	sim := simTestBackend(testAddr)
	t.Cleanup(func() { require.NoError(t, sim.Close()) })

	rpcServer, _ := sim.Node().RPCHandler()
	httpServer := httptest.NewServer(rpcServer)
	t.Cleanup(func() {
		rpcServer.Stop()
		httpServer.Close()
	})

	auth, _ := bind.NewKeyedTransactorWithChainID(testKey, big.NewInt(1337))

	return &httpLogClientTestEnv{
		ctx:        ctx,
		t:          t,
		sim:        sim,
		httpServer: httpServer,
		auth:       auth,
	}
}

// deployContract deploys the test contract for HTTP log client testing.
func (env *httpLogClientTestEnv) deployContract() (*bind.BoundContract, error) {
	contract, err := env.deployCallableContract()
	if err != nil {
		return nil, err
	}
	return contract, nil
}

// deployCallableContract deploys the callable test contract.
func (env *httpLogClientTestEnv) deployCallableContract() (*bind.BoundContract, error) {
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

// createBlocksWithLogs creates blocks with transaction logs.
func (env *httpLogClientTestEnv) createBlocksWithLogs(contract *bind.BoundContract, count int) error {
	for i := 0; i < count; i++ {
		_, err := contract.Transact(env.auth, "Call")
		if err != nil {
			return err
		}
		env.sim.Commit()
	}
	return nil
}

// TestNewHTTPLogClient tests the NewHTTPLogClient constructor function.
func TestNewHTTPLogClient(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("creates client with valid address", func(t *testing.T) {
		client := NewHTTPLogClient("http://localhost:8545", logger)
		require.NotNil(t, client)

		httpLogClient := client.(*HTTPLogClient)
		require.Equal(t, "http://localhost:8545", httpLogClient.httpAddr)
		require.Equal(t, logger, httpLogClient.logger)
		require.False(t, httpLogClient.connected.Load())
	})

	t.Run("returns nil with empty address", func(t *testing.T) {
		client := NewHTTPLogClient("", logger)
		require.Nil(t, client)
	})

	t.Run("normalizes websocket URLs", func(t *testing.T) {
		testCases := []struct {
			input    string
			expected string
		}{
			{"ws://localhost:8546", "http://localhost:8546"},
			{"wss://localhost:8546", "https://localhost:8546"},
			{"http://localhost:8545", "http://localhost:8545"},
			{"https://localhost:8545", "https://localhost:8545"},
		}

		for _, tc := range testCases {
			client := NewHTTPLogClient(tc.input, logger)
			require.NotNil(t, client)
			httpLogClient := client.(*HTTPLogClient)
			require.Equal(t, tc.expected, httpLogClient.httpAddr)
		}
	})
}

// TestHTTPLogClient_Connect tests the Connect method of the HTTP log client.
func TestHTTPLogClient_Connect(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("successfully connects to HTTP endpoint", func(t *testing.T) {
		env := setupHTTPLogClientTestEnv(t, 5*time.Second)

		client := NewHTTPLogClient(env.httpServer.URL, logger)
		require.NotNil(t, client)

		err := client.Connect(env.ctx, 2*time.Second)
		require.NoError(t, err)

		httpLogClient := client.(*HTTPLogClient)
		require.True(t, httpLogClient.connected.Load())
		require.NotNil(t, httpLogClient.client)

		require.NoError(t, client.Close())
	})

	t.Run("fails to connect to invalid endpoint", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()

		client := NewHTTPLogClient("http://127.0.0.1:1", logger)
		require.NotNil(t, client)

		err := client.Connect(ctx, 100*time.Millisecond)
		if err == nil {
			httpLogClient := client.(*HTTPLogClient)
			require.True(t, httpLogClient.connected.Load())

			_, err = client.FetchLogs(ctx, ethcommon.Address{}, 1)
			require.Error(t, err)
		} else {
			require.ErrorContains(t, err, "http log client dial failed")
			httpLogClient := client.(*HTTPLogClient)
			require.False(t, httpLogClient.connected.Load())
		}
	})

	t.Run("returns early if already connected", func(t *testing.T) {
		env := setupHTTPLogClientTestEnv(t, 5*time.Second)

		client := NewHTTPLogClient(env.httpServer.URL, logger)
		require.NotNil(t, client)

		err := client.Connect(env.ctx, 2*time.Second)
		require.NoError(t, err)

		httpLogClient := client.(*HTTPLogClient)
		firstClient := httpLogClient.client
		require.True(t, httpLogClient.connected.Load())

		err = client.Connect(env.ctx, 2*time.Second)
		require.NoError(t, err)
		require.Same(t, firstClient, httpLogClient.client)

		require.NoError(t, client.Close())
	})

	t.Run("respects context timeout", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		client := NewHTTPLogClient("http://127.0.0.1:1", logger)
		require.NotNil(t, client)

		err := client.Connect(ctx, 1*time.Second)
		if err == nil {
			_, err = client.FetchLogs(ctx, ethcommon.Address{}, 1)
			require.Error(t, err)
		} else {
			require.ErrorContains(t, err, "http log client dial failed")
		}
	})

	t.Run("lazy connect - ensures connection during fetch operations", func(t *testing.T) {
		env := setupHTTPLogClientTestEnv(t, 5*time.Second)

		contract, err := env.deployContract()
		require.NoError(t, err)

		err = env.createBlocksWithLogs(contract, 1)
		require.NoError(t, err)

		client := NewHTTPLogClient(env.httpServer.URL, logger)
		require.NotNil(t, client)
		defer client.Close()

		httpLogClient := client.(*HTTPLogClient)

		// First call establishes connection via lazy connect
		_, err = client.FetchLogs(env.ctx, env.contractAddr, 1)
		require.NoError(t, err)
		require.True(t, httpLogClient.connected.Load(), "should be connected after first fetch")
		firstClient := httpLogClient.client

		// Second call reuses existing connection
		_, err = client.FetchLogsViaReceipts(env.ctx, env.contractAddr, 1)
		require.NoError(t, err)
		require.Same(t, firstClient, httpLogClient.client, "should reuse existing connection")
	})
}

// TestHTTPLogClient_FetchLogs tests the FetchLogs method of the HTTP log client.
func TestHTTPLogClient_FetchLogs(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("successfully fetches logs", func(t *testing.T) {
		env := setupHTTPLogClientTestEnv(t, 5*time.Second)

		// Deploy contract and create logs
		contract, err := env.deployContract()
		require.NoError(t, err)

		// Get the current block before creating logs
		currentBlockBefore, err := env.sim.Client().BlockNumber(env.ctx)
		require.NoError(t, err)

		err = env.createBlocksWithLogs(contract, 3)
		require.NoError(t, err)

		// Get the current block after creating logs
		currentBlockAfter, err := env.sim.Client().BlockNumber(env.ctx)
		require.NoError(t, err)

		// Create and connect the HTTP log client
		client := NewHTTPLogClient(env.httpServer.URL, logger)
		require.NotNil(t, client)

		err = client.Connect(env.ctx, 2*time.Second)
		require.NoError(t, err)
		defer client.Close()

		// Fetch logs from the first block that should contain our transaction logs
		// After deployment (block 1), our transactions start at block 2
		blockWithLogs := currentBlockBefore + 1
		logs, err := client.FetchLogs(env.ctx, env.contractAddr, blockWithLogs)
		require.NoError(t, err)
		require.Len(t, logs, 1, "expected 1 log in block %d (range: %d-%d)", blockWithLogs, currentBlockBefore, currentBlockAfter)
		require.Equal(t, env.contractAddr, logs[0].Address)
		require.Equal(t, blockWithLogs, logs[0].BlockNumber)
	})

	t.Run("returns empty logs for block with no matching events", func(t *testing.T) {
		env := setupHTTPLogClientTestEnv(t, 5*time.Second)

		// Deploy contract but don't create any logs
		_, err := env.deployContract()
		require.NoError(t, err)

		// Create empty blocks
		env.sim.Commit()
		env.sim.Commit()

		// Create and connect the HTTP log client
		client := NewHTTPLogClient(env.httpServer.URL, logger)
		require.NotNil(t, client)

		err = client.Connect(env.ctx, 2*time.Second)
		require.NoError(t, err)
		defer client.Close()

		// Fetch logs from block 2 (empty block)
		logs, err := client.FetchLogs(env.ctx, env.contractAddr, 2)
		require.NoError(t, err)
		require.Empty(t, logs)
	})

	t.Run("fails when cannot connect to invalid endpoint", func(t *testing.T) {
		client := NewHTTPLogClient("http://127.0.0.1:1", logger)
		require.NotNil(t, client)

		logs, err := client.FetchLogs(t.Context(), ethcommon.Address{}, 1)
		require.Error(t, err)
		require.ErrorContains(t, err, "connection refused")
		require.Nil(t, logs)
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		env := setupHTTPLogClientTestEnv(t, 5*time.Second)

		_, err := env.deployContract()
		require.NoError(t, err)

		client := NewHTTPLogClient(env.httpServer.URL, logger)
		require.NotNil(t, client)

		err = client.Connect(env.ctx, 2*time.Second)
		require.NoError(t, err)
		defer client.Close()

		cancelledCtx, cancel := context.WithCancel(t.Context())
		cancel()

		logs, err := client.FetchLogs(cancelledCtx, env.contractAddr, 1)
		require.Error(t, err)
		require.Nil(t, logs)
	})
}

// TestHTTPLogClient_FetchLogsViaReceipts tests the FetchLogsViaReceipts method of the HTTP log client.
func TestHTTPLogClient_FetchLogsViaReceipts(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("successfully fetches logs via receipts", func(t *testing.T) {
		env := setupHTTPLogClientTestEnv(t, 5*time.Second)

		// Deploy contract and create logs
		contract, err := env.deployContract()
		require.NoError(t, err)

		// Get the current block before creating logs
		currentBlockBefore, err := env.sim.Client().BlockNumber(env.ctx)
		require.NoError(t, err)

		err = env.createBlocksWithLogs(contract, 2)
		require.NoError(t, err)

		// Get the current block after creating logs
		currentBlockAfter, err := env.sim.Client().BlockNumber(env.ctx)
		require.NoError(t, err)

		// Create and connect the HTTP log client
		client := NewHTTPLogClient(env.httpServer.URL, logger)
		require.NotNil(t, client)

		err = client.Connect(env.ctx, 2*time.Second)
		require.NoError(t, err)
		defer client.Close()

		// Fetch logs from the first block that should contain our transaction logs
		blockWithLogs := currentBlockBefore + 1
		logs, err := client.FetchLogsViaReceipts(env.ctx, env.contractAddr, blockWithLogs)
		require.NoError(t, err)
		require.Len(t, logs, 1, "expected 1 log in block %d (range: %d-%d)", blockWithLogs, currentBlockBefore, currentBlockAfter)
		require.Equal(t, env.contractAddr, logs[0].Address)
		require.Equal(t, blockWithLogs, logs[0].BlockNumber)
	})

	t.Run("returns empty logs for block with no transactions", func(t *testing.T) {
		env := setupHTTPLogClientTestEnv(t, 5*time.Second)

		// Deploy contract
		_, err := env.deployContract()
		require.NoError(t, err)

		// Create an empty block
		env.sim.Commit()

		// Create and connect the HTTP log client
		client := NewHTTPLogClient(env.httpServer.URL, logger)
		require.NotNil(t, client)

		err = client.Connect(env.ctx, 2*time.Second)
		require.NoError(t, err)
		defer client.Close()

		// Fetch logs from an empty block
		logs, err := client.FetchLogsViaReceipts(env.ctx, env.contractAddr, 2)
		require.NoError(t, err)
		require.Empty(t, logs)
	})

	t.Run("fails when cannot connect to invalid endpoint", func(t *testing.T) {
		client := NewHTTPLogClient("http://127.0.0.1:1", logger)
		require.NotNil(t, client)

		logs, err := client.FetchLogsViaReceipts(t.Context(), ethcommon.Address{}, 1)
		require.Error(t, err)
		require.ErrorContains(t, err, "connection refused")
		require.Nil(t, logs)
	})

	t.Run("lazy connect - automatically establishes connection", func(t *testing.T) {
		env := setupHTTPLogClientTestEnv(t, 5*time.Second)

		// Deploy contract and create logs
		contract, err := env.deployContract()
		require.NoError(t, err)

		currentBlockBefore, err := env.sim.Client().BlockNumber(env.ctx)
		require.NoError(t, err)

		err = env.createBlocksWithLogs(contract, 1)
		require.NoError(t, err)

		// Create an HTTP log client but DO NOT call Connect()
		client := NewHTTPLogClient(env.httpServer.URL, logger)
		require.NotNil(t, client)
		defer client.Close()

		httpLogClient := client.(*HTTPLogClient)
		require.False(t, httpLogClient.connected.Load())

		// Call FetchLogsViaReceipts without explicit Connect() - should auto-connect
		blockWithLogs := currentBlockBefore + 1
		logs, err := client.FetchLogsViaReceipts(env.ctx, env.contractAddr, blockWithLogs)
		require.NoError(t, err)
		require.Len(t, logs, 1)

		// Verify connection was established
		require.True(t, httpLogClient.connected.Load())
		require.NotNil(t, httpLogClient.client)
	})

	t.Run("lazy connect - fails with invalid URL", func(t *testing.T) {
		// Use truly invalid protocol to force dial failure
		client := NewHTTPLogClient("invalid://invalid.url", logger)
		require.NotNil(t, client)

		httpLogClient := client.(*HTTPLogClient)
		require.False(t, httpLogClient.connected.Load())

		// FetchLogsViaReceipts should fail during lazy connect
		_, err := client.FetchLogsViaReceipts(t.Context(), ethcommon.Address{}, 1)
		require.Error(t, err)
		require.ErrorContains(t, err, "http log client dial failed")

		// Should still not be connected
		require.False(t, httpLogClient.connected.Load())
	})

	t.Run("handles non-existent block", func(t *testing.T) {
		env := setupHTTPLogClientTestEnv(t, 5*time.Second)

		// Deploy contract
		_, err := env.deployContract()
		require.NoError(t, err)

		// Create and connect the HTTP log client
		client := NewHTTPLogClient(env.httpServer.URL, logger)
		require.NotNil(t, client)

		err = client.Connect(env.ctx, 2*time.Second)
		require.NoError(t, err)
		defer client.Close()

		// Try to fetch logs from a non-existent block
		logs, err := client.FetchLogsViaReceipts(env.ctx, env.contractAddr, 999999)
		require.Error(t, err)
		require.ErrorContains(t, err, "failed to fetch block")
		require.Nil(t, logs)
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		env := setupHTTPLogClientTestEnv(t, 5*time.Second)

		_, err := env.deployContract()
		require.NoError(t, err)

		client := NewHTTPLogClient(env.httpServer.URL, logger)
		require.NotNil(t, client)

		err = client.Connect(env.ctx, 2*time.Second)
		require.NoError(t, err)
		defer client.Close()

		cancelledCtx, cancel := context.WithCancel(t.Context())
		cancel()

		logs, err := client.FetchLogsViaReceipts(cancelledCtx, env.contractAddr, 1)
		require.Error(t, err)
		require.Nil(t, logs)
	})
}

// TestHTTPLogClient_Close tests the Close method of the HTTP log client.
func TestHTTPLogClient_Close(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("successfully closes connected client", func(t *testing.T) {
		env := setupHTTPLogClientTestEnv(t, 5*time.Second)

		client := NewHTTPLogClient(env.httpServer.URL, logger)
		require.NotNil(t, client)

		err := client.Connect(env.ctx, 2*time.Second)
		require.NoError(t, err)

		httpLogClient := client.(*HTTPLogClient)
		require.True(t, httpLogClient.connected.Load())
		require.NotNil(t, httpLogClient.client)

		err = client.Close()
		require.NoError(t, err)
		require.False(t, httpLogClient.connected.Load())
		require.Nil(t, httpLogClient.client)
	})

	t.Run("successfully closes non-connected client", func(t *testing.T) {
		client := NewHTTPLogClient("http://localhost:8545", logger)
		require.NotNil(t, client)

		httpLogClient := client.(*HTTPLogClient)
		require.False(t, httpLogClient.connected.Load())

		err := client.Close()
		require.NoError(t, err)
		require.False(t, httpLogClient.connected.Load())
	})

	t.Run("handles multiple close calls", func(t *testing.T) {
		env := setupHTTPLogClientTestEnv(t, 5*time.Second)

		client := NewHTTPLogClient(env.httpServer.URL, logger)
		require.NotNil(t, client)

		err := client.Connect(env.ctx, 2*time.Second)
		require.NoError(t, err)

		err = client.Close()
		require.NoError(t, err)

		err = client.Close()
		require.NoError(t, err)

		err = client.Close()
		require.NoError(t, err)
	})
}

// TestHTTPLogClient_ConcurrentAccess tests concurrent access patterns of the HTTP log client.
func TestHTTPLogClient_ConcurrentAccess(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("handles concurrent connect and close operations", func(t *testing.T) {
		env := setupHTTPLogClientTestEnv(t, 10*time.Second)

		client := NewHTTPLogClient(env.httpServer.URL, logger)
		require.NotNil(t, client)

		var wg sync.WaitGroup
		numRoutines := 10

		for i := 0; i < numRoutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				err := client.Connect(env.ctx, 1*time.Second)
				if err != nil {
					return
				}

				time.Sleep(10 * time.Millisecond)

				_ = client.Close()
			}()
		}

		wg.Wait()

		require.NoError(t, client.Close())
	})

	t.Run("handles concurrent fetch operations", func(t *testing.T) {
		env := setupHTTPLogClientTestEnv(t, 10*time.Second)

		contract, err := env.deployContract()
		require.NoError(t, err)

		err = env.createBlocksWithLogs(contract, 5)
		require.NoError(t, err)

		client := NewHTTPLogClient(env.httpServer.URL, logger)
		require.NotNil(t, client)

		err = client.Connect(env.ctx, 2*time.Second)
		require.NoError(t, err)
		defer client.Close()

		var wg sync.WaitGroup
		numRoutines := 5

		for i := 0; i < numRoutines; i++ {
			wg.Add(1)
			go func(blockNum uint64) {
				defer wg.Done()

				_, err1 := client.FetchLogs(env.ctx, env.contractAddr, blockNum)
				_, err2 := client.FetchLogsViaReceipts(env.ctx, env.contractAddr, blockNum)

				require.True(t, err1 == nil || err2 == nil)
			}(uint64(i%5 + 1))
		}

		wg.Wait()
	})
}
