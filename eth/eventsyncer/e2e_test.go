package eventsyncer

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
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
	"github.com/ssvlabs/ssv/utils/blskeygen"
	"github.com/ssvlabs/ssv/utils/threshold"
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
	receiptsEmpty       atomic.Bool
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
		case req.Method == "eth_getBlockReceipts" && p.receiptsEmpty.Load():
			// A broken receipt store: zero receipts for a block the caller knows holds logs.
			return jsonrpcMsg{Version: "2.0", ID: req.ID, Result: json.RawMessage(`[]`)}
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

// TestE2E_EmptyReceiptsNotTreatedAsAuthoritative: when eth_getBlockReceipts returns zero receipts
// for a block the sync recorded logs for, that's a broken receipt store, not proof the block is
// empty — the verifier must park (no authoritative source), not treat it as a confirmed miss and
// trigger a false wipe+resync.
func TestE2E_EmptyReceiptsNotTreatedAsAuthoritative(t *testing.T) {
	ctx := t.Context()
	es, nodeStorage, proxy, _ := setupVerifyE2E(t, ctx, 3)

	// Optimistic sync records all 3 operators (digests written).
	_, err := es.SyncHistory(ctx, 0, false)
	require.NoError(t, err)
	require.Equal(t, 3, operatorCount(t, nodeStorage))

	// The log index drops the middle block AND receipts come back empty (a broken receipt store):
	// the verify-time fetch disagrees with the recorded digest, and receipts can't authoritatively
	// resolve it.
	proxy.dropActive.Store(true)
	proxy.receiptsEmpty.Store(true)

	require.NoError(t, es.Verify(ctx), "empty receipts must not be treated as an authoritative empty block")
	resync, err := nodeStorage.IsResyncRequired(nil)
	require.NoError(t, err)
	require.False(t, resync, "an inconsistent empty-receipts response must not trigger a resync")
	ranges, err := nodeStorage.ListUnverifiedRanges(nil)
	require.NoError(t, err)
	require.NotEmpty(t, ranges, "range parked (no authoritative source), not retired or resynced")
}

// TestE2E_ParkedRangeResolvesWhenReceiptsReturn: a range parked because the receipt store was
// transiently broken (returned empty) is re-checked and cleared once receipts come back. Unlike an
// unsupported-method (-32601) park — which latches and only clears on restart — an empty-receipts
// park doesn't latch, so a later Verify pass resolves it in-process. Here the recorded digest is
// vindicated by the receipts (the drop was only in the log index), so the range retires with no resync.
func TestE2E_ParkedRangeResolvesWhenReceiptsReturn(t *testing.T) {
	ctx := t.Context()
	es, nodeStorage, proxy, _ := setupVerifyE2E(t, ctx, 3)

	// Optimistic sync records all 3 operators.
	_, err := es.SyncHistory(ctx, 0, false)
	require.NoError(t, err)
	require.Equal(t, 3, operatorCount(t, nodeStorage))

	// Log index drops the middle block and the receipt store returns empty → the disagreeing block
	// can't be resolved authoritatively, so the range parks (without latching receipts unsupported).
	proxy.dropActive.Store(true)
	proxy.receiptsEmpty.Store(true)
	require.NoError(t, es.Verify(ctx))
	ranges, err := nodeStorage.ListUnverifiedRanges(nil)
	require.NoError(t, err)
	require.NotEmpty(t, ranges, "range parked while the receipt store was broken")

	// Receipts come back. The next Verify resolves the parked block against them; the recorded
	// digest matches the receipts truth, so the range retires clean with no resync.
	proxy.receiptsEmpty.Store(false)
	require.NoError(t, es.Verify(ctx))
	resync, err := nodeStorage.IsResyncRequired(nil)
	require.NoError(t, err)
	require.False(t, resync, "recorded digest vindicated by receipts → no resync")
	ranges, err = nodeStorage.ListUnverifiedRanges(nil)
	require.NoError(t, err)
	require.Empty(t, ranges, "parked range retired once receipts became available")
}

// setupClusterDriftE2E builds the drift scenario from the #2990 field report
// (https://github.com/ssvlabs/ssv/issues/2990#issuecomment-5339378358), where a dropped block
// window was found to lose every registry event type it contained, not just OperatorAdded.
// It registers four operators — the node's own operator first, so the sync learns its ID and
// treats the validator below as its own — then:
//
//   - a kept block: a valid ValidatorAdded for the four-operator cluster owned by testAddr
//     (bumps the owner nonce to 1, stores the share, and adds it to the node's key manager);
//   - the block the proxy will drop: a FeeRecipientAddressUpdated, a ClusterLiquidated for that
//     cluster, and a second ValidatorAdded (deliberately malformed: the handler skips it but
//     still bumps the owner nonce, which is how the field report's stale-nonce drift arises).
//
// It returns the syncer, storage, proxy (drop not yet active), the validator public key, and
// the updated fee recipient.
func setupClusterDriftE2E(t *testing.T, ctx context.Context) (*EventSyncer, operatorstorage.Storage, *dropProxy, []byte, ethcommon.Address) {
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

	// Four operators, one per block; index 0 is the node's own key, so the sync assigns the node
	// operator ID 1 and the validator below decrypts as its own share.
	opKeys := make([]keys.OperatorPrivateKey, 4)
	for i := range opKeys {
		opKeys[i], err = keys.GeneratePrivateKey()
		require.NoError(t, err)
		pubKey, err := opKeys[i].Public().Base64()
		require.NoError(t, err)
		packed, err := eventparser.PackOperatorPublicKey(pubKey)
		require.NoError(t, err)
		_, err = boundContract.RegisterOperator(auth, packed, big.NewInt(100_000_000))
		require.NoError(t, err)
		sim.Commit()
	}

	// A valid validator for cluster {1,2,3,4}: a real BLS master key split into four shares, each
	// encrypted to its operator, with the owner-nonce signature the handler verifies (nonce 0 —
	// the owner's first registration).
	threshold.Init()
	masterSK, masterPK := blskeygen.GenBLSKeyPair()
	shareKeys, err := threshold.Create(masterSK.Serialize(), 3, 4)
	require.NoError(t, err)

	operatorIDs := []uint64{1, 2, 3, 4}
	sharePubKeys := make([]byte, 0, len(opKeys)*phase0.PublicKeyLength)
	encryptedShares := make([]byte, 0, len(opKeys)*256)
	for i, opKey := range opKeys {
		shareSK := shareKeys[uint64(i+1)]
		cipher, err := opKey.Public().Encrypt([]byte(shareSK.SerializeToHexStr()))
		require.NoError(t, err)
		sharePubKeys = append(sharePubKeys, shareSK.GetPublicKey().Serialize()...)
		encryptedShares = append(encryptedShares, cipher...)
	}
	ownerNonceHash := crypto.Keccak256([]byte(fmt.Sprintf("%s:%d", testAddr.String(), 0)))
	sharesData := masterSK.SignByte(ownerNonceHash).Serialize()
	sharesData = append(sharesData, sharePubKeys...)
	sharesData = append(sharesData, encryptedShares...)

	cluster := simcontract.CallableCluster{
		ValidatorCount:  1,
		NetworkFeeIndex: 1,
		Index:           1,
		Active:          true,
		Balance:         big.NewInt(100_000_000),
	}
	_, err = boundContract.RegisterValidator(auth, masterPK.Serialize(), operatorIDs, sharesData, big.NewInt(100_000_000), cluster)
	require.NoError(t, err)
	sim.Commit()

	// The to-be-dropped block: three registry events of different types in one block.
	updatedRecipient := ethcommon.HexToAddress("0x1111111111111111111111111111111111111111")
	_, err = boundContract.SetFeeRecipientAddress(auth, updatedRecipient)
	require.NoError(t, err)
	_, err = boundContract.Liquidate(auth, testAddr, operatorIDs, cluster)
	require.NoError(t, err)
	// Deliberately malformed (wrong shares length): the handler skips it as malformed but still
	// bumps the owner nonce, so dropping this block leaves a stale nonce behind.
	_, err = boundContract.RegisterValidator(auth, bytes.Repeat([]byte{0xaa}, 48), operatorIDs, []byte("malformed"), big.NewInt(100_000_000), cluster)
	require.NoError(t, err)
	sim.Commit()
	dropBlock, err := sim.Client().BlockNumber(ctx)
	require.NoError(t, err)

	// Pad past the follow distance so the dropped block is within the historical window.
	for i := 0; i < executionclient.FollowDistance+2; i++ {
		sim.Commit()
	}

	proxyURL, proxy := newDropProxy(t, rpcServer, dropBlock)

	db, err := kv.NewInMemory(logger, basedb.Options{Ctx: ctx})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	nodeStorage, operatorData := setupOperatorStorage(logger, db, opKeys[0])
	eh := setupEventHandler(t, ctx, logger, db, nodeStorage, operatorData, opKeys[0])

	execClient, err := executionclient.New(ctx, proxyURL.URL, contractAddr, executionclient.WithLogger(logger))
	require.NoError(t, err)
	t.Cleanup(func() { _ = execClient.Close() })
	require.NoError(t, execClient.Healthy(ctx))

	es := New(nodeStorage, execClient, eh, WithLogger(logger))
	es.verifyChunkDelay = 0

	return es, nodeStorage, proxy, masterPK.Serialize(), updatedRecipient
}

// TestE2E_DriftBeyondOperatorsRepaired pins the full blast radius documented in the #2990 field
// report: a dropped block loses whatever registry events it contained — here a fee-recipient
// update, a cluster liquidation, and an owner-nonce bump — and the detect→repair loop restores
// all of them. The validator belongs to the node's own operator, so the verified resync also
// replays ValidatorAdded against a key manager that still holds the share account (the repair
// drops registry state, never signer state) — pinning that AddShare tolerates the replay.
func TestE2E_DriftBeyondOperatorsRepaired(t *testing.T) {
	ctx := t.Context()
	es, nodeStorage, proxy, validatorPK, updatedRecipient := setupClusterDriftE2E(t, ctx)
	proxy.dropActive.Store(true) // persistently drop the fee-recipient/liquidation/nonce block

	// Optimistic sync: the dropped block's three events are silently missing, so the node runs on
	// drifted state — stale nonce, unliquidated share, default fee recipient — as in the field report.
	_, err := es.SyncHistory(ctx, 0, false)
	require.NoError(t, err)
	require.Equal(t, 4, operatorCount(t, nodeStorage))

	share, found := nodeStorage.Shares().Get(nil, validatorPK)
	require.True(t, found, "the validator from the kept block must be stored")
	require.True(t, share.BelongsToOperator(1), "the node is operator 1, so this is its own share")
	require.False(t, share.Liquidated, "drift: the ClusterLiquidated was silently missed")

	// The kept registration's nonce bump created the default recipient record (feeRecipient ==
	// owner); the dropped update never moved it off the default — the field report's exact drift.
	recipient, err := nodeStorage.GetFeeRecipient(testAddr)
	require.NoError(t, err)
	require.Equal(t, testAddr.Bytes(), recipient[:], "drift: the FeeRecipientAddressUpdated was silently missed, recipient is still the owner default")

	nonce, err := nodeStorage.GetNextNonce(nil, testAddr)
	require.NoError(t, err)
	require.EqualValues(t, 1, nonce, "drift: the dropped registration's nonce bump was silently missed")

	// The background verifier recovers the block from receipts and flags the repair.
	require.ErrorIs(t, es.Verify(ctx), ErrResyncRequired)

	// Repair as the boot path does: drop registry state + journal (never the key manager) and
	// resync with inline verification, which recovers the dropped block from receipts.
	require.NoError(t, nodeStorage.DropRegistryData())
	require.NoError(t, nodeStorage.DropVerificationJournal())
	_, err = es.SyncHistory(ctx, 0, true)
	require.NoError(t, err, "the resync must tolerate replaying ValidatorAdded over the existing share account")

	// Every drifted class converges to canonical state.
	share, found = nodeStorage.Shares().Get(nil, validatorPK)
	require.True(t, found)
	require.True(t, share.Liquidated, "repair restored the missed liquidation")

	recipient, err = nodeStorage.GetFeeRecipient(testAddr)
	require.NoError(t, err)
	require.Equal(t, updatedRecipient.Bytes(), recipient[:], "repair restored the missed fee-recipient update")

	nonce, err = nodeStorage.GetNextNonce(nil, testAddr)
	require.NoError(t, err)
	require.EqualValues(t, 2, nonce, "repair restored the missed nonce bump")

	require.NoError(t, es.Verify(ctx), "a fresh verification pass over the verified resync is clean")
}
