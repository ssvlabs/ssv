package eventsyncer

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/ssvlabs/ssv/eth/eventparser"
	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/eth/simulator/simcontract"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/ssvsigner/keys/rsaencryption"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// These tests drive the real historical-sync + background-verification + repair pipeline
// (ExecutionClient → EventSyncer → EventHandler → BadgerDB) against a real go-ethereum RPC stack
// (the simulated backend), with an HTTP JSON-RPC proxy in front that induces the exact #2990
// failure — an incomplete eth_getLogs response — so we can watch the fix detect and repair it end
// to end. Only the EL is in-process; every log/header/receipt is served by a real RPC handler.

// dropProxy fronts an RPC handler and, while active, removes one block's logs from every
// eth_getLogs response (range and single-block), optionally also failing eth_getBlockReceipts —
// modeling a persistently-broken log index, with or without a usable receipt store.
type dropProxy struct {
	dropBlock           uint64
	dropActive          atomic.Bool
	receiptsUnavailable atomic.Bool
}

type jsonrpcMsg struct {
	Version string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

func newDropProxy(t *testing.T, base http.Handler, dropBlock uint64) (*httptest.Server, *dropProxy) {
	p := &dropProxy{dropBlock: dropBlock}

	forward := func(req jsonrpcMsg) jsonrpcMsg {
		body, err := json.Marshal(req)
		require.NoError(t, err)
		rec := httptest.NewRecorder()
		httpReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		base.ServeHTTP(rec, httpReq)
		var resp jsonrpcMsg
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		return resp
	}

	serve := func(req jsonrpcMsg) jsonrpcMsg {
		switch {
		case req.Method == "eth_getBlockReceipts" && p.receiptsUnavailable.Load():
			// -32601 = method not found; makes the client treat receipts as unsupported.
			return jsonrpcMsg{Version: "2.0", ID: req.ID, Error: json.RawMessage(`{"code":-32601,"message":"the method eth_getBlockReceipts does not exist"}`)}
		case req.Method == "eth_getLogs" && p.dropActive.Load():
			resp := forward(req)
			resp.Result = dropBlockLogs(t, resp.Result, p.dropBlock)
			return resp
		default:
			return forward(req)
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		// A batch request is a JSON array; serve each element through the same logic.
		if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
			var reqs []jsonrpcMsg
			require.NoError(t, json.Unmarshal(raw, &reqs))
			resps := make([]jsonrpcMsg, 0, len(reqs))
			for _, req := range reqs {
				resps = append(resps, serve(req))
			}
			require.NoError(t, json.NewEncoder(w).Encode(resps))
			return
		}
		var req jsonrpcMsg
		require.NoError(t, json.Unmarshal(raw, &req))
		require.NoError(t, json.NewEncoder(w).Encode(serve(req)))
	}))
	t.Cleanup(srv.Close)
	return srv, p
}

// dropBlockLogs removes all logs of the given block from an eth_getLogs result.
func dropBlockLogs(t *testing.T, result json.RawMessage, block uint64) json.RawMessage {
	if len(result) == 0 {
		return result
	}
	var logs []map[string]any
	require.NoError(t, json.Unmarshal(result, &logs))
	kept := make([]map[string]any, 0, len(logs))
	for _, l := range logs {
		num, err := hexutil.DecodeUint64(l["blockNumber"].(string))
		require.NoError(t, err)
		if num != block {
			kept = append(kept, l)
		}
	}
	out, err := json.Marshal(kept)
	require.NoError(t, err)
	return out
}

func operatorCount(t *testing.T, s operatorstorage.Storage) int {
	ops, err := s.ListOperators(nil, 0, 0)
	require.NoError(t, err)
	return len(ops)
}

// setupVerifyE2E deploys the contract, registers operatorsN operators (one per block), pads past
// the follow distance, and wires the real pipeline through a drop-proxy targeting the middle
// operator's block. It returns the syncer, storage, proxy, and that middle block number.
func setupVerifyE2E(t *testing.T, ctx context.Context, operatorsN int) (*EventSyncer, operatorstorage.Storage, *dropProxy, uint64) {
	logger := zaptest.NewLogger(t)

	sim := simTestBackend(testAddr)
	rpcServer, err := sim.Node().RPCHandler()
	require.NoError(t, err)
	t.Cleanup(rpcServer.Stop)

	parsed, err := abi.JSON(strings.NewReader(simcontract.SimcontractMetaData.ABI))
	require.NoError(t, err)
	auth, err := bind.NewKeyedTransactorWithChainID(testKey, big.NewInt(1337))
	require.NoError(t, err)
	contractAddr, _, _, err := bind.DeployContract(auth, parsed, ethcommon.FromHex(simcontract.SimcontractMetaData.Bin), sim.Client())
	require.NoError(t, err)
	sim.Commit()

	boundContract, err := simcontract.NewSimcontract(contractAddr, sim.Client())
	require.NoError(t, err)

	// Register operatorsN operators, one per block; remember the middle one's block to drop.
	var midBlock uint64
	for i := 0; i < operatorsN; i++ {
		pubKey, _, err := rsaencryption.GenerateKeyPairPEM()
		require.NoError(t, err)
		packed, err := eventparser.PackOperatorPublicKey(base64.StdEncoding.EncodeToString(pubKey))
		require.NoError(t, err)
		_, err = boundContract.RegisterOperator(auth, packed, big.NewInt(100_000_000))
		require.NoError(t, err)
		sim.Commit()
		if i == operatorsN/2 {
			midBlock, err = sim.Client().BlockNumber(ctx)
			require.NoError(t, err)
		}
	}
	// Pad past the follow distance so all operator blocks are within the historical window.
	for i := 0; i < executionclient.FollowDistance+2; i++ {
		sim.Commit()
	}

	proxyURL, proxy := newDropProxy(t, rpcServer, midBlock)

	db, err := kv.NewInMemory(logger, basedb.Options{Ctx: ctx})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	privateKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)
	nodeStorage, operatorData := setupOperatorStorage(logger, db, privateKey)
	eh := setupEventHandler(t, ctx, logger, db, nodeStorage, operatorData, privateKey)

	execClient, err := executionclient.New(ctx, proxyURL.URL, contractAddr, executionclient.WithLogger(logger))
	require.NoError(t, err)
	t.Cleanup(func() { _ = execClient.Close() })
	require.NoError(t, execClient.Healthy(ctx))

	es := New(nodeStorage, execClient, eh, WithLogger(logger))
	es.verifyChunkDelay = 0

	return es, nodeStorage, proxy, midBlock
}

// TestE2E_HealthyEL_VerifiesClean is the no-regression baseline: against a healthy EL the
// optimistic sync stores every operator and the background verifier retires the range clean —
// no false-positive resync.
func TestE2E_HealthyEL_VerifiesClean(t *testing.T) {
	ctx := t.Context()
	es, nodeStorage, _, _ := setupVerifyE2E(t, ctx, 3)

	_, err := es.SyncHistory(ctx, 0, false) // optimistic (cold-sync path)
	require.NoError(t, err)
	require.Equal(t, 3, operatorCount(t, nodeStorage))

	require.NoError(t, es.Verify(ctx))

	resync, err := nodeStorage.IsResyncRequired(nil)
	require.NoError(t, err)
	require.False(t, resync)
	ranges, err := nodeStorage.ListUnverifiedRanges(nil)
	require.NoError(t, err)
	require.Empty(t, ranges, "clean range should be retired")
}

// TestE2E_DropDetectedAndRepaired is the #2990 end-to-end: a persistently-lying EL drops one
// operator's logs, the optimistic sync silently misses it, the background verifier recovers the
// block from receipts and flags a resync, and the verified resync rebuilds complete state.
func TestE2E_DropDetectedAndRepaired(t *testing.T) {
	ctx := t.Context()
	es, nodeStorage, proxy, dropped := setupVerifyE2E(t, ctx, 3)
	proxy.dropActive.Store(true) // persistently drop the middle operator's block from getLogs

	// Optimistic sync silently loses the dropped operator — the #2990 failure.
	_, err := es.SyncHistory(ctx, 0, false)
	require.NoError(t, err)
	require.Equal(t, 2, operatorCount(t, nodeStorage), "one operator silently missed (block %d dropped)", dropped)

	// Background verification recovers the block via receipts, sees the mismatch, flags a resync.
	require.ErrorIs(t, es.Verify(ctx), ErrResyncRequired)
	resync, err := nodeStorage.IsResyncRequired(nil)
	require.NoError(t, err)
	require.True(t, resync)

	// Repair (what the boot path does): drop registry + journal, then resync with inline
	// verification — which recovers the dropped block from receipts.
	require.NoError(t, nodeStorage.DropRegistryData())
	require.NoError(t, nodeStorage.DropVerificationJournal())
	_, err = es.SyncHistory(ctx, 0, true) // verify=true (inline)
	require.NoError(t, err)

	require.Equal(t, 3, operatorCount(t, nodeStorage), "resync recovered the dropped operator")

	// A fresh verification pass over the (verified) resync finds nothing to do.
	require.NoError(t, es.Verify(ctx))
}

// TestE2E_ParksWhenReceiptsUnavailable: when a disagreement can't be resolved against receipts
// (EL without eth_getBlockReceipts), the verifier parks the range — no resync on a guess, and the
// range stays visible/pending rather than being retired as verified.
func TestE2E_ParksWhenReceiptsUnavailable(t *testing.T) {
	ctx := t.Context()
	es, nodeStorage, proxy, _ := setupVerifyE2E(t, ctx, 3)

	// Optimistic sync sees the full chain (all 3 operators recorded, digests written).
	_, err := es.SyncHistory(ctx, 0, false)
	require.NoError(t, err)
	require.Equal(t, 3, operatorCount(t, nodeStorage))

	// Now the EL's log index breaks for that block AND receipts become unavailable — so the
	// verify-time fetch disagrees with the recorded digest and there's no authoritative source.
	proxy.dropActive.Store(true)
	proxy.receiptsUnavailable.Store(true)

	require.NoError(t, es.Verify(ctx), "unresolvable disagreement must not resync")

	resync, err := nodeStorage.IsResyncRequired(nil)
	require.NoError(t, err)
	require.False(t, resync, "must not resync without an authoritative source")

	ranges, err := nodeStorage.ListUnverifiedRanges(nil)
	require.NoError(t, err)
	require.NotEmpty(t, ranges, "range stays pending (parked), not retired as verified")
}
