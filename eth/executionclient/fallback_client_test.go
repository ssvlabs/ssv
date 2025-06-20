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

// fallbackTestEnv provides test environment for fallback client testing.
type fallbackTestEnv struct {
	ctx          context.Context
	t            *testing.T
	sim          *simulator.Backend
	httpServer   *httptest.Server
	contractAddr ethcommon.Address
	auth         *bind.TransactOpts
}

// setupFallbackTestEnv creates a new test environment for fallback client testing.
func setupFallbackTestEnv(t *testing.T, testTimeout time.Duration) *fallbackTestEnv {
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

	return &fallbackTestEnv{
		ctx:        ctx,
		t:          t,
		sim:        sim,
		httpServer: httpServer,
		auth:       auth,
	}
}

// deployContract deploys the test contract for fallback testing.
func (env *fallbackTestEnv) deployContract() (*bind.BoundContract, error) {
	contract, err := env.deployCallableContract()
	if err != nil {
		return nil, err
	}
	return contract, nil
}

// deployCallableContract deploys the callable test contract.
func (env *fallbackTestEnv) deployCallableContract() (*bind.BoundContract, error) {
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
func (env *fallbackTestEnv) createBlocksWithLogs(contract *bind.BoundContract, count int) error {
	for i := 0; i < count; i++ {
		_, err := contract.Transact(env.auth, "Call")
		if err != nil {
			return err
		}
		env.sim.Commit()
	}
	return nil
}

// TestNewFallbackClient tests the NewFallbackClient constructor function.
func TestNewFallbackClient(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("creates client with valid address", func(t *testing.T) {
		client := NewFallbackClient("http://localhost:8545", logger)
		require.NotNil(t, client)

		fallbackClient := client.(*FallbackClient)
		require.Equal(t, "http://localhost:8545", fallbackClient.httpAddr)
		require.Equal(t, logger, fallbackClient.logger)
		require.False(t, fallbackClient.connected.Load())
	})

	t.Run("returns nil with empty address", func(t *testing.T) {
		client := NewFallbackClient("", logger)
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
			client := NewFallbackClient(tc.input, logger)
			require.NotNil(t, client)
			fallbackClient := client.(*FallbackClient)
			require.Equal(t, tc.expected, fallbackClient.httpAddr)
		}
	})
}

// TestFallbackClient_Connect tests the Connect method of the fallback client.
func TestFallbackClient_Connect(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("successfully connects to HTTP endpoint", func(t *testing.T) {
		env := setupFallbackTestEnv(t, 5*time.Second)

		client := NewFallbackClient(env.httpServer.URL, logger)
		require.NotNil(t, client)

		err := client.Connect(env.ctx, 2*time.Second)
		require.NoError(t, err)

		fallbackClient := client.(*FallbackClient)
		require.True(t, fallbackClient.connected.Load())
		require.NotNil(t, fallbackClient.client)

		require.NoError(t, client.Close())
	})

	t.Run("fails to connect to invalid endpoint", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()

		client := NewFallbackClient("http://127.0.0.1:1", logger)
		require.NotNil(t, client)

		err := client.Connect(ctx, 100*time.Millisecond)
		if err == nil {
			fallbackClient := client.(*FallbackClient)
			require.True(t, fallbackClient.connected.Load())

			_, err = client.FetchLogs(ctx, ethcommon.Address{}, 1)
			require.Error(t, err)
		} else {
			require.ErrorContains(t, err, "http fallback dial failed")
			fallbackClient := client.(*FallbackClient)
			require.False(t, fallbackClient.connected.Load())
		}
	})

	t.Run("returns early if already connected", func(t *testing.T) {
		env := setupFallbackTestEnv(t, 5*time.Second)

		client := NewFallbackClient(env.httpServer.URL, logger)
		require.NotNil(t, client)

		err := client.Connect(env.ctx, 2*time.Second)
		require.NoError(t, err)

		fallbackClient := client.(*FallbackClient)
		firstClient := fallbackClient.client
		require.True(t, fallbackClient.connected.Load())

		err = client.Connect(env.ctx, 2*time.Second)
		require.NoError(t, err)
		require.Same(t, firstClient, fallbackClient.client)

		require.NoError(t, client.Close())
	})

	t.Run("respects context timeout", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		client := NewFallbackClient("http://127.0.0.1:1", logger)
		require.NotNil(t, client)

		err := client.Connect(ctx, 1*time.Second)
		if err == nil {
			_, err = client.FetchLogs(ctx, ethcommon.Address{}, 1)
			require.Error(t, err)
		} else {
			require.ErrorContains(t, err, "http fallback dial failed")
		}
	})
}

// TestFallbackClient_FetchLogs tests the FetchLogs method of the fallback client.
func TestFallbackClient_FetchLogs(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("successfully fetches logs", func(t *testing.T) {
		env := setupFallbackTestEnv(t, 5*time.Second)

		// Deploy contract and create logs
		contract, err := env.deployContract()
		require.NoError(t, err)

		// Get current block before creating logs
		currentBlockBefore, err := env.sim.Client().BlockNumber(env.ctx)
		require.NoError(t, err)

		err = env.createBlocksWithLogs(contract, 3)
		require.NoError(t, err)

		// Get current block after creating logs
		currentBlockAfter, err := env.sim.Client().BlockNumber(env.ctx)
		require.NoError(t, err)

		// Create and connect fallback client
		client := NewFallbackClient(env.httpServer.URL, logger)
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
		env := setupFallbackTestEnv(t, 5*time.Second)

		// Deploy contract but don't create any logs
		_, err := env.deployContract()
		require.NoError(t, err)

		// Create empty blocks
		env.sim.Commit()
		env.sim.Commit()

		// Create and connect fallback client
		client := NewFallbackClient(env.httpServer.URL, logger)
		require.NotNil(t, client)

		err = client.Connect(env.ctx, 2*time.Second)
		require.NoError(t, err)
		defer client.Close()

		// Fetch logs from block 2 (empty block)
		logs, err := client.FetchLogs(env.ctx, env.contractAddr, 2)
		require.NoError(t, err)
		require.Empty(t, logs)
	})

	t.Run("fails when not connected", func(t *testing.T) {
		client := NewFallbackClient("http://localhost:8545", logger)
		require.NotNil(t, client)

		logs, err := client.FetchLogs(t.Context(), ethcommon.Address{}, 1)
		require.Error(t, err)
		require.ErrorContains(t, err, "http fallback not connected")
		require.Nil(t, logs)
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		env := setupFallbackTestEnv(t, 5*time.Second)

		_, err := env.deployContract()
		require.NoError(t, err)

		client := NewFallbackClient(env.httpServer.URL, logger)
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

// TestFallbackClient_FetchLogsViaReceipts tests the FetchLogsViaReceipts method of the fallback client.
func TestFallbackClient_FetchLogsViaReceipts(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("successfully fetches logs via receipts", func(t *testing.T) {
		env := setupFallbackTestEnv(t, 5*time.Second)

		// Deploy contract and create logs
		contract, err := env.deployContract()
		require.NoError(t, err)

		// Get current block before creating logs
		currentBlockBefore, err := env.sim.Client().BlockNumber(env.ctx)
		require.NoError(t, err)

		err = env.createBlocksWithLogs(contract, 2)
		require.NoError(t, err)

		// Get current block after creating logs
		currentBlockAfter, err := env.sim.Client().BlockNumber(env.ctx)
		require.NoError(t, err)

		// Create and connect fallback client
		client := NewFallbackClient(env.httpServer.URL, logger)
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
		env := setupFallbackTestEnv(t, 5*time.Second)

		// Deploy contract
		_, err := env.deployContract()
		require.NoError(t, err)

		// Create empty block
		env.sim.Commit()

		// Create and connect fallback client
		client := NewFallbackClient(env.httpServer.URL, logger)
		require.NotNil(t, client)

		err = client.Connect(env.ctx, 2*time.Second)
		require.NoError(t, err)
		defer client.Close()

		// Fetch logs from empty block
		logs, err := client.FetchLogsViaReceipts(env.ctx, env.contractAddr, 2)
		require.NoError(t, err)
		require.Empty(t, logs)
	})

	t.Run("fails when not connected", func(t *testing.T) {
		client := NewFallbackClient("http://localhost:8545", logger)
		require.NotNil(t, client)

		logs, err := client.FetchLogsViaReceipts(t.Context(), ethcommon.Address{}, 1)
		require.Error(t, err)
		require.ErrorContains(t, err, "http fallback not connected")
		require.Nil(t, logs)
	})

	t.Run("handles non-existent block", func(t *testing.T) {
		env := setupFallbackTestEnv(t, 5*time.Second)

		// Deploy contract
		_, err := env.deployContract()
		require.NoError(t, err)

		// Create and connect fallback client
		client := NewFallbackClient(env.httpServer.URL, logger)
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
		env := setupFallbackTestEnv(t, 5*time.Second)

		_, err := env.deployContract()
		require.NoError(t, err)

		client := NewFallbackClient(env.httpServer.URL, logger)
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

// TestFallbackClient_Close tests the Close method of the fallback client.
func TestFallbackClient_Close(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("successfully closes connected client", func(t *testing.T) {
		env := setupFallbackTestEnv(t, 5*time.Second)

		client := NewFallbackClient(env.httpServer.URL, logger)
		require.NotNil(t, client)

		err := client.Connect(env.ctx, 2*time.Second)
		require.NoError(t, err)

		fallbackClient := client.(*FallbackClient)
		require.True(t, fallbackClient.connected.Load())
		require.NotNil(t, fallbackClient.client)

		err = client.Close()
		require.NoError(t, err)
		require.False(t, fallbackClient.connected.Load())
		require.Nil(t, fallbackClient.client)
	})

	t.Run("successfully closes non-connected client", func(t *testing.T) {
		client := NewFallbackClient("http://localhost:8545", logger)
		require.NotNil(t, client)

		fallbackClient := client.(*FallbackClient)
		require.False(t, fallbackClient.connected.Load())

		err := client.Close()
		require.NoError(t, err)
		require.False(t, fallbackClient.connected.Load())
	})

	t.Run("handles multiple close calls", func(t *testing.T) {
		env := setupFallbackTestEnv(t, 5*time.Second)

		client := NewFallbackClient(env.httpServer.URL, logger)
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

// TestFallbackClient_ConcurrentAccess tests concurrent access patterns of the fallback client.
func TestFallbackClient_ConcurrentAccess(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("handles concurrent connect and close operations", func(t *testing.T) {
		env := setupFallbackTestEnv(t, 10*time.Second)

		client := NewFallbackClient(env.httpServer.URL, logger)
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
		env := setupFallbackTestEnv(t, 10*time.Second)

		contract, err := env.deployContract()
		require.NoError(t, err)

		err = env.createBlocksWithLogs(contract, 5)
		require.NoError(t, err)

		client := NewFallbackClient(env.httpServer.URL, logger)
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
