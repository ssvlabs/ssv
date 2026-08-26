package executionclient

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

const (
	ethGetLogsMethod          = "eth_getLogs"
	ethGetBlockByNumberMethod = "eth_getBlockByNumber"
	ethGetBlockReceiptsMethod = "eth_getBlockReceipts"
)

// jsonrpcMessage is a minimal JSON-RPC 2.0 envelope used by the test proxy.
type jsonrpcMessage struct {
	Version string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

// forwardFunc relays a single JSON-RPC request to the real backend and returns its response.
type forwardFunc func(req jsonrpcMessage) jsonrpcMessage

// newRPCProxy serves JSON-RPC over HTTP in front of the given backend handler, routing
// every request — including each element of a batch request — through handle. The handler
// may respond itself (returning a message whose ID is filled in by the proxy), forward the
// request via the provided forwardFunc and tamper with the response, or return nil to
// forward the request untouched.
func newRPCProxy(t *testing.T, base http.Handler, handle func(req jsonrpcMessage, forward forwardFunc) *jsonrpcMessage) *httptest.Server {
	forward := func(req jsonrpcMessage) jsonrpcMessage {
		body, err := json.Marshal(req)
		require.NoError(t, err)
		httpReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		base.ServeHTTP(rec, httpReq)
		var resp jsonrpcMessage
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		return resp
	}

	serve := func(req jsonrpcMessage) jsonrpcMessage {
		if resp := handle(req, forward); resp != nil {
			resp.Version = "2.0"
			resp.ID = req.ID
			return *resp
		}
		return forward(req)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")

		if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
			var reqs []jsonrpcMessage
			require.NoError(t, json.Unmarshal(raw, &reqs))
			resps := make([]jsonrpcMessage, 0, len(reqs))
			for _, req := range reqs {
				resps = append(resps, serve(req))
			}
			require.NoError(t, json.NewEncoder(w).Encode(resps))
			return
		}

		var req jsonrpcMessage
		require.NoError(t, json.Unmarshal(raw, &req))
		require.NoError(t, json.NewEncoder(w).Encode(serve(req)))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// getLogsRange extracts the block range of an eth_getLogs request.
func getLogsRange(t *testing.T, params json.RawMessage) (from, to uint64) {
	var filters []map[string]any
	require.NoError(t, json.Unmarshal(params, &filters))
	require.NotEmpty(t, filters)
	fromHex, _ := filters[0]["fromBlock"].(string)
	toHex, _ := filters[0]["toBlock"].(string)
	from, err := hexutil.DecodeUint64(fromHex)
	require.NoError(t, err)
	to, err = hexutil.DecodeUint64(toHex)
	require.NoError(t, err)
	return from, to
}

// dropLogsForBlock removes all logs of the given block from an eth_getLogs result.
func dropLogsForBlock(t *testing.T, result json.RawMessage, blockNumber uint64) json.RawMessage {
	var logs []map[string]any
	require.NoError(t, json.Unmarshal(result, &logs))
	kept := make([]map[string]any, 0, len(logs))
	for _, logEntry := range logs {
		num, err := hexutil.DecodeUint64(logEntry["blockNumber"].(string))
		require.NoError(t, err)
		if num != blockNumber {
			kept = append(kept, logEntry)
		}
	}
	out, err := json.Marshal(kept)
	require.NoError(t, err)
	return out
}

// requestedBlock extracts the block number argument of an eth_getBlockByNumber or
// eth_getBlockReceipts request.
func requestedBlock(t *testing.T, params json.RawMessage) uint64 {
	var args []any
	require.NoError(t, json.Unmarshal(params, &args))
	require.NotEmpty(t, args)
	numHex, ok := args[0].(string)
	require.True(t, ok)
	num, err := hexutil.DecodeUint64(numHex)
	require.NoError(t, err)
	return num
}

// TestBloomCrossCheck_RecoversMissingLogs verifies that the completeness check detects
// and recovers events the EL dropped from a range query but returns on a single-block
// re-request (simulates the intermittent Geth bug).
func TestBloomCrossCheck_RecoversMissingLogs(t *testing.T) {
	logger := zaptest.NewLogger(t)

	env := setupTestEnv(t, 10*time.Second)
	contract, err := env.deployCallableContract()
	require.NoError(t, err)

	// Create 5 blocks with events.
	const totalBlocks = 5
	err = env.createBlocksWithLogs(contract, totalBlocks, 0)
	require.NoError(t, err)

	rpcSrv, _ := env.sim.Node().RPCHandler()

	// The contract deploy is block 1, then transactions are blocks 2..6.
	// We'll drop logs for block 4 from wide queries; single-block re-requests
	// pass through to the real backend and return the truth.
	const dropBlock uint64 = 4
	var getLogsCalls atomic.Int32

	srv := newRPCProxy(t, rpcSrv, func(req jsonrpcMessage, forward forwardFunc) *jsonrpcMessage {
		if req.Method != ethGetLogsMethod {
			return nil
		}
		getLogsCalls.Add(1)
		if from, to := getLogsRange(t, req.Params); from == to {
			return nil // single-block re-request — answer truthfully
		}
		resp := forward(req)
		resp.Result = dropLogsForBlock(t, resp.Result, dropBlock)
		return &resp
	})

	client, err := New(t.Context(), srv.URL, env.contractAddr,
		WithLogger(logger),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	currentBlock, err := client.client.BlockNumber(t.Context())
	require.NoError(t, err)

	logsCh, errCh := client.fetchLogsInBatches(t.Context(), 0, currentBlock, true)

	allLogs := make([]ethtypes.Log, 0, totalBlocks)
	for block := range logsCh {
		allLogs = append(allLogs, block.Logs...)
	}
	require.NoError(t, <-errCh)

	// We should have recovered the dropped block's log, so all 5 events are present.
	require.Equal(t, totalBlocks, len(allLogs), "completeness check should recover the dropped log")

	// The proxy should have seen more than one eth_getLogs:
	// 1 initial wide query + at least 1 single-block re-request.
	require.Greater(t, getLogsCalls.Load(), int32(1), "expected at least one re-request for the suspect block")
}

// TestFetchHistoricalLogs_RecoversDroppedLogs verifies that the historical sync path —
// the one that runs on every node startup — recovers events the EL dropped from a range
// query. This is the regression test for silently losing registry events during
// historical sync (#2990).
func TestFetchHistoricalLogs_RecoversDroppedLogs(t *testing.T) {
	logger := zaptest.NewLogger(t)

	env := setupTestEnv(t, 10*time.Second)
	contract, err := env.deployCallableContract()
	require.NoError(t, err)

	// Contract deploy is block 1, event blocks are 2..6, then FollowDistance padding
	// blocks so the historical fetch window covers all event blocks.
	const totalBlocks = 5
	err = env.createBlocksWithLogs(contract, totalBlocks, 0)
	require.NoError(t, err)
	for i := 0; i < FollowDistance; i++ {
		env.sim.Commit()
	}

	rpcSrv, _ := env.sim.Node().RPCHandler()

	const dropBlock uint64 = 4
	srv := newRPCProxy(t, rpcSrv, func(req jsonrpcMessage, forward forwardFunc) *jsonrpcMessage {
		if req.Method != ethGetLogsMethod {
			return nil
		}
		if from, to := getLogsRange(t, req.Params); from == to {
			return nil
		}
		resp := forward(req)
		resp.Result = dropLogsForBlock(t, resp.Result, dropBlock)
		return &resp
	})

	client, err := New(t.Context(), srv.URL, env.contractAddr,
		WithLogger(logger),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	logsCh, errsCh, err := client.FetchHistoricalLogs(t.Context(), 0, true)
	require.NoError(t, err)

	allLogs := make([]ethtypes.Log, 0, totalBlocks)
	for block := range logsCh {
		allLogs = append(allLogs, block.Logs...)
	}
	require.NoError(t, <-errsCh)

	require.Equal(t, totalBlocks, len(allLogs), "historical sync should recover the dropped log")
	recoveredDropped := false
	for _, logEntry := range allLogs {
		if logEntry.BlockNumber == dropBlock {
			recoveredDropped = true
		}
	}
	require.True(t, recoveredDropped, "the dropped block's event should be present")
}

// TestReceiptsCrossCheck_RecoversPersistentlyDroppedLogs verifies that when the EL never
// returns a block's logs from eth_getLogs (e.g. a broken log index) — so re-requests
// cannot recover them — the events are recovered from the block's receipts.
func TestReceiptsCrossCheck_RecoversPersistentlyDroppedLogs(t *testing.T) {
	logger := zaptest.NewLogger(t)

	env := setupTestEnv(t, 10*time.Second)
	contract, err := env.deployCallableContract()
	require.NoError(t, err)

	const totalBlocks = 5
	err = env.createBlocksWithLogs(contract, totalBlocks, 0)
	require.NoError(t, err)

	rpcSrv, _ := env.sim.Node().RPCHandler()

	// Drop block 4's logs from every eth_getLogs response, wide or single-block,
	// simulating a persistently broken log index. Receipts pass through untouched.
	const dropBlock uint64 = 4
	var receiptsCalls atomic.Int32

	srv := newRPCProxy(t, rpcSrv, func(req jsonrpcMessage, forward forwardFunc) *jsonrpcMessage {
		switch req.Method {
		case ethGetLogsMethod:
			resp := forward(req)
			resp.Result = dropLogsForBlock(t, resp.Result, dropBlock)
			return &resp
		case ethGetBlockReceiptsMethod:
			receiptsCalls.Add(1)
		}
		return nil
	})

	client, err := New(t.Context(), srv.URL, env.contractAddr,
		WithLogger(logger),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	currentBlock, err := client.client.BlockNumber(t.Context())
	require.NoError(t, err)

	logsCh, errCh := client.fetchLogsInBatches(t.Context(), 0, currentBlock, true)

	allLogs := make([]ethtypes.Log, 0, totalBlocks)
	for block := range logsCh {
		allLogs = append(allLogs, block.Logs...)
	}
	require.NoError(t, <-errCh)

	require.Equal(t, totalBlocks, len(allLogs), "receipts cross-check should recover the persistently dropped log")
	recoveredDropped := false
	for _, logEntry := range allLogs {
		if logEntry.BlockNumber == dropBlock {
			recoveredDropped = true
			require.Equal(t, env.contractAddr, logEntry.Address)
		}
	}
	require.True(t, recoveredDropped, "the dropped block's event should be recovered from receipts")
	require.GreaterOrEqual(t, receiptsCalls.Load(), int32(1), "expected a receipts fetch for the suspect block")
	require.False(t, client.receiptsUnsupported.Load())
}

// TestReceiptsUnsupported_FallsBackToTimedRetry verifies that when the EL does not
// support eth_getBlockReceipts, suspect blocks are resolved with timed single-block
// retries (the pre-receipts behavior), and the lack of support is remembered.
func TestReceiptsUnsupported_FallsBackToTimedRetry(t *testing.T) {
	logger := zaptest.NewLogger(t)

	env := setupTestEnv(t, 15*time.Second)
	contract, err := env.deployCallableContract()
	require.NoError(t, err)

	const totalBlocks = 5
	err = env.createBlocksWithLogs(contract, totalBlocks, 0)
	require.NoError(t, err)

	rpcSrv, _ := env.sim.Node().RPCHandler()

	// Drop block 4's logs from wide queries and from the first two single-block
	// requests, then start telling the truth — so only a delayed retry recovers it.
	// Receipts are rejected as an unsupported method.
	const dropBlock uint64 = 4
	var singleBlockCalls atomic.Int32

	srv := newRPCProxy(t, rpcSrv, func(req jsonrpcMessage, forward forwardFunc) *jsonrpcMessage {
		switch req.Method {
		case ethGetLogsMethod:
			from, to := getLogsRange(t, req.Params)
			if from == to {
				if from == dropBlock && singleBlockCalls.Add(1) <= 2 {
					return &jsonrpcMessage{Result: json.RawMessage(`[]`)}
				}
				return nil
			}
			resp := forward(req)
			resp.Result = dropLogsForBlock(t, resp.Result, dropBlock)
			return &resp
		case ethGetBlockReceiptsMethod:
			return &jsonrpcMessage{Error: json.RawMessage(`{"code":-32601,"message":"the method eth_getBlockReceipts does not exist/is not available"}`)}
		}
		return nil
	})

	client, err := New(t.Context(), srv.URL, env.contractAddr,
		WithLogger(logger),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	currentBlock, err := client.client.BlockNumber(t.Context())
	require.NoError(t, err)

	logsCh, errCh := client.fetchLogsInBatches(t.Context(), 0, currentBlock, true)

	allLogs := make([]ethtypes.Log, 0, totalBlocks)
	for block := range logsCh {
		allLogs = append(allLogs, block.Logs...)
	}
	require.NoError(t, <-errCh)

	require.Equal(t, totalBlocks, len(allLogs), "timed retry fallback should recover the dropped log")
	require.True(t, client.receiptsUnsupported.Load(), "unsupported eth_getBlockReceipts should be remembered")
}

// TestBloomFalsePositive_ConfirmedByReceipts verifies that a block whose bloom matches
// the contract without holding any of its events (a bloom false positive) is confirmed
// empty via receipts without injecting spurious logs or failing the fetch.
func TestBloomFalsePositive_ConfirmedByReceipts(t *testing.T) {
	logger := zaptest.NewLogger(t)

	env := setupTestEnv(t, 10*time.Second)
	contract, err := env.deployCallableContract()
	require.NoError(t, err)

	// Blocks 2, 3 have events; block 4 is empty.
	const totalBlocks = 2
	err = env.createBlocksWithLogs(contract, totalBlocks, 0)
	require.NoError(t, err)
	env.sim.Commit()

	rpcSrv, _ := env.sim.Node().RPCHandler()

	// Force block 4's bloom to match everything, making it a guaranteed false positive.
	const falsePositiveBlock uint64 = 4
	var receiptsCalls atomic.Int32
	saturatedBloom := "0x" + strings.Repeat("ff", 256)

	srv := newRPCProxy(t, rpcSrv, func(req jsonrpcMessage, forward forwardFunc) *jsonrpcMessage {
		switch req.Method {
		case ethGetBlockByNumberMethod:
			if requestedBlock(t, req.Params) != falsePositiveBlock {
				return nil
			}
			resp := forward(req)
			var header map[string]any
			require.NoError(t, json.Unmarshal(resp.Result, &header))
			header["logsBloom"] = saturatedBloom
			raw, err := json.Marshal(header)
			require.NoError(t, err)
			resp.Result = raw
			return &resp
		case ethGetBlockReceiptsMethod:
			receiptsCalls.Add(1)
		}
		return nil
	})

	client, err := New(t.Context(), srv.URL, env.contractAddr,
		WithLogger(logger),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	currentBlock, err := client.client.BlockNumber(t.Context())
	require.NoError(t, err)

	logsCh, errCh := client.fetchLogsInBatches(t.Context(), 0, currentBlock, true)

	allLogs := make([]ethtypes.Log, 0, totalBlocks)
	for block := range logsCh {
		allLogs = append(allLogs, block.Logs...)
	}
	require.NoError(t, <-errCh)

	require.Equal(t, totalBlocks, len(allLogs), "a bloom false positive should not inject or drop logs")
	require.GreaterOrEqual(t, receiptsCalls.Load(), int32(1), "the false positive should have been confirmed via receipts")
}

// TestBloomCrossCheck_NoFalseRecovery verifies that when no logs are dropped
// (all blocks genuinely have no events or already have logs), the completeness
// check does not inject spurious events.
func TestBloomCrossCheck_NoFalseRecovery(t *testing.T) {
	logger := zaptest.NewLogger(t)

	env := setupTestEnv(t, 5*time.Second)
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

	currentBlock, err := env.client.client.BlockNumber(t.Context())
	require.NoError(t, err)

	logsCh, errCh := env.client.fetchLogsInBatches(t.Context(), 0, currentBlock, true)

	allLogs := make([]ethtypes.Log, 0, totalBlocks)
	for block := range logsCh {
		allLogs = append(allLogs, block.Logs...)
	}
	require.NoError(t, <-errCh)

	require.Equal(t, totalBlocks, len(allLogs), "should get exactly the expected number of logs")
}

// TestVerifyLogCompleteness_EmptyRange verifies that the completeness check handles
// an empty log slice for a range where no events exist.
func TestVerifyLogCompleteness_EmptyRange(t *testing.T) {
	logger := zaptest.NewLogger(t)

	env := setupTestEnv(t, 5*time.Second)
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

	// Call verifyLogCompleteness directly with empty logs.
	result, err := env.client.verifyLogCompleteness(env.ctx, nil, 2, 6)
	require.NoError(t, err)
	require.Empty(t, result, "empty blocks should produce no logs even after the completeness check")
}

// TestBloomCrossCheck_ResolutionErrorPropagates verifies that when the suspect-block
// re-request keeps failing with RPC errors, the error propagates and no partial data
// is emitted.
func TestBloomCrossCheck_ResolutionErrorPropagates(t *testing.T) {
	logger := zaptest.NewLogger(t)

	env := setupTestEnv(t, 10*time.Second)

	// Deploy contract and create blocks.
	parsed, _ := abi.JSON(strings.NewReader(callableAbi))
	contractAddr, _, contract, err := bind.DeployContract(
		env.auth,
		parsed,
		ethcommon.FromHex(callableBin),
		env.sim.Client(),
	)
	require.NoError(t, err)
	env.contractAddr = contractAddr
	env.sim.Commit()

	// Create 3 blocks with events.
	for i := 0; i < 3; i++ {
		_, err := contract.Transact(env.auth, "Call")
		require.NoError(t, err)
		env.sim.Commit()
	}

	rpcSrv, _ := env.sim.Node().RPCHandler()

	// Drop block 3's logs from wide queries, and fail every single-block request for it.
	const dropBlock uint64 = 3

	srv := newRPCProxy(t, rpcSrv, func(req jsonrpcMessage, forward forwardFunc) *jsonrpcMessage {
		if req.Method != ethGetLogsMethod {
			return nil
		}
		from, to := getLogsRange(t, req.Params)
		if from == dropBlock && to == dropBlock {
			return &jsonrpcMessage{Error: json.RawMessage(`{"code":-32000,"message":"internal error"}`)}
		}
		if from != to {
			resp := forward(req)
			resp.Result = dropLogsForBlock(t, resp.Result, dropBlock)
			return &resp
		}
		return nil
	})

	client, err := New(t.Context(), srv.URL, contractAddr,
		WithLogger(logger),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	currentBlock, err := client.client.BlockNumber(t.Context())
	require.NoError(t, err)

	logsCh, errCh := client.fetchLogsInBatches(t.Context(), 0, currentBlock, true)

	// Drain logsCh — should close quickly because of error.
	for range logsCh {
	}

	err = <-errCh
	require.Error(t, err, "should propagate the re-request failure as an error")
}

// TestVerifyLogCompleteness_BlocksWithExistingLogs verifies that blocks that already
// have logs are not re-checked via header fetch.
func TestVerifyLogCompleteness_BlocksWithExistingLogs(t *testing.T) {
	logger := zaptest.NewLogger(t)

	env := setupTestEnv(t, 5*time.Second)
	contract, err := env.deployCallableContract()
	require.NoError(t, err)

	const totalBlocks = 3
	err = env.createBlocksWithLogs(contract, totalBlocks, 0)
	require.NoError(t, err)

	// Track header fetches (batched or not) via proxy.
	rpcSrv, _ := env.sim.Node().RPCHandler()
	var headerCalls atomic.Int32

	srv := newRPCProxy(t, rpcSrv, func(req jsonrpcMessage, forward forwardFunc) *jsonrpcMessage {
		if req.Method == ethGetBlockByNumberMethod {
			headerCalls.Add(1)
		}
		return nil
	})

	client, err := New(t.Context(), srv.URL, env.contractAddr,
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
	result, err := client.verifyLogCompleteness(t.Context(), fakeLogs, 2, 4)
	require.NoError(t, err)
	require.Equal(t, 3, len(result))

	// No headers should have been fetched since all blocks already had logs.
	require.Equal(t, int32(0), headerCalls.Load(), "should not fetch headers for blocks that already have logs")
}

// TestVerifyLogs_RecoversViaReceipts verifies the background verifier's per-chunk verification:
// it returns a block's logs from receipts even when eth_getLogs persistently omits them,
// and screens empty blocks by bloom so it doesn't fetch receipts for every block.
func TestVerifyLogs_RecoversViaReceipts(t *testing.T) {
	logger := zaptest.NewLogger(t)

	env := setupTestEnv(t, 10*time.Second)
	contract, err := env.deployCallableContract()
	require.NoError(t, err)

	const totalBlocks = 5
	err = env.createBlocksWithLogs(contract, totalBlocks, 0)
	require.NoError(t, err)

	rpcSrv, _ := env.sim.Node().RPCHandler()

	// Drop block 4's logs from every eth_getLogs response (persistent log-index breakage);
	// receipts pass through untouched.
	const dropBlock uint64 = 4
	var receiptsCalls atomic.Int32

	srv := newRPCProxy(t, rpcSrv, func(req jsonrpcMessage, forward forwardFunc) *jsonrpcMessage {
		switch req.Method {
		case ethGetLogsMethod:
			resp := forward(req)
			resp.Result = dropLogsForBlock(t, resp.Result, dropBlock)
			return &resp
		case ethGetBlockReceiptsMethod:
			receiptsCalls.Add(1)
		}
		return nil
	})

	client, err := New(t.Context(), srv.URL, env.contractAddr, WithLogger(logger))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	currentBlock, err := client.client.BlockNumber(t.Context())
	require.NoError(t, err)

	blockLogs, err := client.VerifyLogs(t.Context(), 0, currentBlock)
	require.NoError(t, err)

	total := 0
	foundDropped := false
	for _, bl := range blockLogs {
		total += len(bl.Logs)
		if bl.BlockNumber == dropBlock {
			foundDropped = true
		}
	}
	require.Equal(t, totalBlocks, total, "VerifyLogs should return every block's logs, including the getLogs-dropped one")
	require.True(t, foundDropped, "the dropped block's logs must be recovered from receipts")
	// Receipts are fetched only for bloom-positive blocks, not every block in the range.
	require.LessOrEqual(t, receiptsCalls.Load(), int32(totalBlocks+1))
	require.GreaterOrEqual(t, receiptsCalls.Load(), int32(1))
}

// newBatchRejectingProxy fronts base but rejects JSON-RPC batch (array) requests with an HTTP
// error while forwarding single requests untouched — simulating a provider that disables
// batching, to exercise the sequential fallback in HeadersByNumbers / SingleBlockLogs.
func newBatchRejectingProxy(t *testing.T, base http.Handler) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
			http.Error(w, "batch requests are disabled", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		base.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestHeadersByNumbers_FallsBackWhenBatchRejected(t *testing.T) {
	env := setupTestEnv(t, 10*time.Second)
	for i := 0; i < 5; i++ {
		env.sim.Commit()
	}

	rpcSrv, _ := env.sim.Node().RPCHandler()
	srv := newBatchRejectingProxy(t, rpcSrv)

	client, err := New(t.Context(), srv.URL, env.contractAddr, WithLogger(zaptest.NewLogger(t)))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	// The batch is rejected, so this must recover every header via sequential fallback.
	blocks := []uint64{1, 2, 3}
	headers, err := client.client.HeadersByNumbers(t.Context(), blocks)
	require.NoError(t, err)
	require.Len(t, headers, len(blocks))
	for i, h := range headers {
		require.NotNil(t, h)
		require.Equal(t, blocks[i], h.Number.Uint64())
	}
}

func TestSingleBlockLogs_FallsBackWhenBatchRejected(t *testing.T) {
	env := setupTestEnv(t, 10*time.Second)
	contract, err := env.deployCallableContract()
	require.NoError(t, err)
	// Contract deploy is block 1; these create event blocks 2..4, one contract log each.
	require.NoError(t, env.createBlocksWithLogs(contract, 3, 0))

	rpcSrv, _ := env.sim.Node().RPCHandler()
	srv := newBatchRejectingProxy(t, rpcSrv)

	client, err := New(t.Context(), srv.URL, env.contractAddr, WithLogger(zaptest.NewLogger(t)))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	// The batch is rejected, so each block's logs must come via sequential FilterLogs fallback.
	blocks := []uint64{1, 2, 3, 4}
	perBlock, err := client.client.SingleBlockLogs(t.Context(), env.contractAddr, blocks)
	require.NoError(t, err)
	require.Len(t, perBlock, len(blocks))

	total := 0
	for _, logs := range perBlock {
		total += len(logs)
	}
	require.Equal(t, 3, total, "one contract log recovered per event block (2..4), none for the deploy block")
	require.Len(t, perBlock[1], 1) // block 2
	require.Len(t, perBlock[2], 1) // block 3
	require.Len(t, perBlock[3], 1) // block 4
}

func TestBatchingUnsupportedIsRemembered(t *testing.T) {
	env := setupTestEnv(t, 10*time.Second)
	for i := 0; i < 5; i++ {
		env.sim.Commit()
	}
	rpcSrv, _ := env.sim.Node().RPCHandler()

	// Reject every batch (array) request, counting the attempts; forward singles.
	var batchAttempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
			batchAttempts.Add(1)
			http.Error(w, "batch requests are disabled", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		rpcSrv.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	client, err := New(t.Context(), srv.URL, env.contractAddr, WithLogger(zaptest.NewLogger(t)))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	// First call attempts a batch once, fails wholesale, recovers sequentially, and remembers.
	_, err = client.client.HeadersByNumbers(t.Context(), []uint64{1, 2, 3})
	require.NoError(t, err)
	// Second call must not attempt a batch again — it goes straight to sequential.
	_, err = client.client.HeadersByNumbers(t.Context(), []uint64{1, 2, 3})
	require.NoError(t, err)

	require.Equal(t, int32(1), batchAttempts.Load(), "batching should be attempted once then remembered as unsupported")
}
