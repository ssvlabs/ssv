package executionclient

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

const ethGetLogsMethod = "eth_getLogs"

// TestBloomCrossCheck_RecoversMissingLogs verifies that the bloom filter cross-check
// detects and recovers events that the EL silently dropped (simulates the Geth bug).
func TestBloomCrossCheck_RecoversMissingLogs(t *testing.T) {
	logger := zaptest.NewLogger(t)

	env := setupTestEnv(t)
	contract, err := env.deployCallableContract()
	require.NoError(t, err)

	// Create 5 blocks with events.
	const totalBlocks = 5
	err = env.createBlocksWithLogs(contract, totalBlocks, 0)
	require.NoError(t, err)

	// Set up a proxy that drops logs for one specific block.
	rpcSrv, _ := env.sim.Node().RPCHandler()
	base := http.Handler(rpcSrv)

	// The contract deploy is block 1, then transactions are blocks 2..6.
	// We'll drop logs for block 4.
	const dropBlock uint64 = 4
	var filterCallCount atomic.Int32

	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var req map[string]any
		_ = json.Unmarshal(raw, &req)

		if req["method"] == ethGetLogsMethod {
			filterCallCount.Add(1)

			if from, to := getLogsBlockRange(req); to > from {
				// Wide query — drop logs for dropBlock. Single-block retries pass through.
				serveGetLogsWithoutBlock(w, r, base, dropBlock)
				return
			}
		}

		base.ServeHTTP(w, r)
	})

	srv := httptest.NewServer(wrapped)
	t.Cleanup(srv.Close)

	client, err := New(env.ctx, srv.URL, env.contractAddr,
		WithLogger(logger),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	// Use fetchLogsInBatches directly with verifyBloom=true (FetchHistoricalLogs skips bloom checks).
	currentBlock, err := client.client.BlockNumber(env.ctx)
	require.NoError(t, err)

	logsCh, errCh := client.fetchLogsInBatches(env.ctx, 0, currentBlock, true)

	allLogs := make([]ethtypes.Log, 0, totalBlocks)
	for block := range logsCh {
		allLogs = append(allLogs, block.Logs...)
	}
	require.NoError(t, <-errCh)

	// We should have recovered the dropped block's log, so all 5 events are present.
	require.Equal(t, totalBlocks, len(allLogs), "bloom cross-check should recover the dropped log")

	// The proxy should have been called more than once for eth_getLogs:
	// 1 initial wide query + at least 1 single-block retry.
	require.Greater(t, filterCallCount.Load(), int32(1), "expected at least one retry for bloom mismatch")
}

// TestBloomCrossCheck_NoFalseRecovery verifies that when no logs are dropped
// (all blocks genuinely have no events or already have logs), the bloom check
// does not inject spurious events.
func TestBloomCrossCheck_NoFalseRecovery(t *testing.T) {
	logger := zaptest.NewLogger(t)

	env := setupTestEnv(t)
	contract, err := env.deployCallableContract()
	require.NoError(t, err)

	const totalBlocks = 5
	err = env.createBlocksWithLogs(contract, totalBlocks, 0)
	require.NoError(t, err)

	// No proxy tricks — use the real backend directly.
	err = env.createClient(
		WithLogger(logger),
	)
	require.NoError(t, err)

	// Use fetchLogsInBatches directly with verifyBloom=true to actually exercise bloom checks.
	currentBlock, err := env.client.client.BlockNumber(env.ctx)
	require.NoError(t, err)

	logsCh, errCh := env.client.fetchLogsInBatches(env.ctx, 0, currentBlock, true)

	allLogs := make([]ethtypes.Log, 0, totalBlocks)
	for block := range logsCh {
		allLogs = append(allLogs, block.Logs...)
	}
	require.NoError(t, <-errCh)

	require.Equal(t, totalBlocks, len(allLogs), "should get exactly the expected number of logs")
}

// TestVerifyLogsWithBloom_EmptyRange verifies that bloom verification handles
// an empty log slice for a range where no events exist.
func TestVerifyLogsWithBloom_EmptyRange(t *testing.T) {
	logger := zaptest.NewLogger(t)

	env := setupTestEnv(t)
	_, err := env.deployCallableContract()
	require.NoError(t, err)

	// Create empty blocks (no contract calls).
	for i := 0; i < 5; i++ {
		env.sim.Commit()
	}

	err = env.createClient(
		WithLogger(logger),
	)
	require.NoError(t, err)

	// Call verifyLogsWithBloom directly with empty logs.
	result, err := env.client.verifyLogsWithBloom(env.ctx, nil, 2, 6)
	require.NoError(t, err)
	require.Empty(t, result, "empty blocks should produce no logs even after bloom check")
}

// TestBloomCrossCheck_RetryExhausted verifies that when retries fail with RPC errors,
// the error propagates and no partial data is emitted.
func TestBloomCrossCheck_RetryExhausted(t *testing.T) {
	logger := zaptest.NewLogger(t)

	env := setupTestEnv(t)

	// Deploy contract and create 3 blocks with events.
	contract, err := env.deployCallableContract()
	require.NoError(t, err)
	err = env.createBlocksWithLogs(contract, 3, 0)
	require.NoError(t, err)

	// Proxy: drop logs for block 3 on wide queries, and return HTTP 500 for single-block retries of block 3.
	rpcSrv, _ := env.sim.Node().RPCHandler()
	base := http.Handler(rpcSrv)

	const dropBlock uint64 = 3

	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var req map[string]any
		_ = json.Unmarshal(raw, &req)

		if req["method"] == ethGetLogsMethod {
			from, to := getLogsBlockRange(req)

			if from == dropBlock && to == dropBlock {
				// Single-block retry — return error.
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			if to > from {
				// Wide query — drop logs for dropBlock.
				serveGetLogsWithoutBlock(w, r, base, dropBlock)
				return
			}
		}

		base.ServeHTTP(w, r)
	})

	srv := httptest.NewServer(wrapped)
	t.Cleanup(srv.Close)

	client, err := New(env.ctx, srv.URL, env.contractAddr,
		WithLogger(logger),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	// Every retry fails the same way, so don't wait between them.
	client.bloomRetryDelay = 0

	// Use fetchLogsInBatches directly with verifyBloom=true (FetchHistoricalLogs skips bloom checks).
	currentBlock, err := client.client.BlockNumber(env.ctx)
	require.NoError(t, err)

	logsCh, errCh := client.fetchLogsInBatches(env.ctx, 0, currentBlock, true)

	// Drain logsCh — should close quickly because of error.
	for range logsCh {
	}

	err = <-errCh
	require.Error(t, err, "should propagate retry failure as error")
}

// TestVerifyLogsWithBloom_BlocksWithExistingLogs verifies that blocks
// that already have logs are not re-checked via header fetch.
func TestVerifyLogsWithBloom_BlocksWithExistingLogs(t *testing.T) {
	logger := zaptest.NewLogger(t)

	env := setupTestEnv(t)
	contract, err := env.deployCallableContract()
	require.NoError(t, err)

	const totalBlocks = 3
	err = env.createBlocksWithLogs(contract, totalBlocks, 0)
	require.NoError(t, err)

	// Track HeaderByNumber calls via proxy.
	rpcSrv, _ := env.sim.Node().RPCHandler()
	base := http.Handler(rpcSrv)
	var headerCalls atomic.Int32

	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(raw, &req)

		if req["method"] == "eth_getBlockByNumber" {
			headerCalls.Add(1)
		}

		r.Body = io.NopCloser(bytes.NewReader(raw))
		base.ServeHTTP(w, r)
	})

	srv := httptest.NewServer(wrapped)
	t.Cleanup(srv.Close)

	client, err := New(env.ctx, srv.URL, env.contractAddr,
		WithLogger(logger),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	// Manually build logs that cover blocks 2, 3, 4 (contract deploy is block 1).
	fakeLogs := []ethtypes.Log{
		{BlockNumber: 2},
		{BlockNumber: 3},
		{BlockNumber: 4},
	}

	headerCalls.Store(0)
	result, err := client.verifyLogsWithBloom(env.ctx, fakeLogs, 2, 4)
	require.NoError(t, err)
	require.Equal(t, 3, len(result))

	// No headers should have been fetched since all blocks already had logs.
	require.Equal(t, int32(0), headerCalls.Load(), "should not fetch headers for blocks that already have logs")
}

// serveGetLogsWithoutBlock forwards an eth_getLogs request to base and strips dropBlock's
// logs from the response, simulating an EL that silently omits logs (the Geth bug).
func serveGetLogsWithoutBlock(w http.ResponseWriter, r *http.Request, base http.Handler, dropBlock uint64) {
	rec := httptest.NewRecorder()
	base.ServeHTTP(rec, r)

	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)

	if result, ok := resp["result"].([]any); ok {
		filtered := make([]any, 0, len(result))
		for _, entry := range result {
			blockNum, _ := hexutil.DecodeUint64(entry.(map[string]any)["blockNumber"].(string))
			if blockNum != dropBlock {
				filtered = append(filtered, entry)
			}
		}
		resp["result"] = filtered
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
