package eventsyncer

import (
	"context"
	"errors"
	"testing"
	"time"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/networkconfig"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// verifierHarness wires the syncer's background verifier to a mocked execution client (so we
// control the authoritative logs) and real in-memory storage (so digests/ranges round-trip).
type verifierHarness struct {
	es      *EventSyncer
	exec    *MockExecutionClient
	storage operatorstorage.Storage
}

func newVerifierHarness(t *testing.T) *verifierHarness {
	logger := zaptest.NewLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{Ctx: t.Context()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	nodeStorage, err := operatorstorage.NewNodeStorage(networkconfig.TestNetwork.Beacon, logger, db)
	require.NoError(t, err)

	exec := NewMockExecutionClient(gomock.NewController(t))
	es := New(nodeStorage, exec, nil, WithLogger(logger))
	es.verifyChunkDelay = 0 // keep multi-chunk tests fast

	return &verifierHarness{es: es, exec: exec, storage: nodeStorage}
}

func logsWith(ids ...[2]uint) []ethtypes.Log {
	logs := make([]ethtypes.Log, len(ids))
	for i, id := range ids {
		logs[i] = ethtypes.Log{TxIndex: id[0], Index: id[1]}
	}
	return logs
}

// expectReceipts mocks the authoritative (receipts-derived) logs for a block, used to resolve a
// digest mismatch.
func (h *verifierHarness) expectReceipts(block uint64, logs []ethtypes.Log) {
	h.exec.EXPECT().BlockContractLogs(gomock.Any(), block).Return(logs, true, nil)
}

// expectNoReceipts mocks the EL reporting eth_getBlockReceipts unavailable for a block.
func (h *verifierHarness) expectNoReceipts(block uint64) {
	h.exec.EXPECT().BlockContractLogs(gomock.Any(), block).Return(nil, false, nil)
}

func TestVerify_NoRanges(t *testing.T) {
	h := newVerifierHarness(t)
	// No ranges journalled: Verify is a no-op and must not touch the execution client.
	require.NoError(t, h.es.Verify(t.Context()))
}

func TestVerify_CleanRange(t *testing.T) {
	h := newVerifierHarness(t)

	logs12 := logsWith([2]uint{0, 0})
	logs15 := logsWith([2]uint{0, 0}, [2]uint{1, 1})
	require.NoError(t, h.storage.SaveBlockLogDigest(nil, 12, executionclient.BlockLogsDigest(logs12)))
	require.NoError(t, h.storage.SaveBlockLogDigest(nil, 15, executionclient.BlockLogsDigest(logs15)))
	require.NoError(t, h.storage.SaveUnverifiedRange(nil, operatorstorage.UnverifiedRange{From: 10, To: 20, Cursor: 10}))

	// Authoritative logs match what the sync recorded, so the range is clean.
	h.exec.EXPECT().VerifyLogs(gomock.Any(), uint64(10), uint64(20)).Return([]executionclient.BlockLogs{
		{BlockNumber: 12, Logs: logs12},
		{BlockNumber: 15, Logs: logs15},
	}, nil)

	require.NoError(t, h.es.Verify(t.Context()))

	// A clean range is removed along with its digests, and no resync is flagged.
	ranges, err := h.storage.ListUnverifiedRanges(nil)
	require.NoError(t, err)
	require.Empty(t, ranges)
	for _, block := range []uint64{12, 15} {
		_, found, err := h.storage.GetBlockLogDigest(nil, block)
		require.NoError(t, err)
		require.False(t, found)
	}
	resync, err := h.storage.IsResyncRequired(nil)
	require.NoError(t, err)
	require.False(t, resync)
}

func TestVerify_ReceiptsConfirmMissingDigest(t *testing.T) {
	h := newVerifierHarness(t)

	// The sync recorded no digest for block 15, VerifyLogs shows contract logs there, and receipts
	// confirm those logs exist — the silent miss #2990 is about. Resync.
	logs := logsWith([2]uint{0, 0})
	require.NoError(t, h.storage.SaveUnverifiedRange(nil, operatorstorage.UnverifiedRange{From: 15, To: 15, Cursor: 15}))
	h.exec.EXPECT().VerifyLogs(gomock.Any(), uint64(15), uint64(15)).Return([]executionclient.BlockLogs{
		{BlockNumber: 15, Logs: logs},
	}, nil)
	h.expectReceipts(15, logs)

	require.ErrorIs(t, h.es.Verify(t.Context()), ErrResyncRequired)

	resync, err := h.storage.IsResyncRequired(nil)
	require.NoError(t, err)
	require.True(t, resync)
}

func TestVerify_ReceiptsConfirmSubsetMiss(t *testing.T) {
	h := newVerifierHarness(t)

	// The sync recorded one log, VerifyLogs shows two, and receipts confirm two: the recorded
	// response was an incomplete subset. Resync.
	recorded := logsWith([2]uint{0, 0})
	truth := logsWith([2]uint{0, 0}, [2]uint{1, 1})
	require.NoError(t, h.storage.SaveBlockLogDigest(nil, 12, executionclient.BlockLogsDigest(recorded)))
	require.NoError(t, h.storage.SaveUnverifiedRange(nil, operatorstorage.UnverifiedRange{From: 12, To: 12, Cursor: 12}))
	h.exec.EXPECT().VerifyLogs(gomock.Any(), uint64(12), uint64(12)).Return([]executionclient.BlockLogs{
		{BlockNumber: 12, Logs: truth},
	}, nil)
	h.expectReceipts(12, truth)

	require.ErrorIs(t, h.es.Verify(t.Context()), ErrResyncRequired)

	resync, err := h.storage.IsResyncRequired(nil)
	require.NoError(t, err)
	require.True(t, resync)
}

func TestVerify_ReceiptsVindicateVerifyTimeDrop(t *testing.T) {
	h := newVerifierHarness(t)

	// The sync recorded logs for block 12, but the verify-time VerifyLogs returns it empty (a
	// verify-time getLogs blip, or a different EL under MultiClient). Receipts confirm the block
	// really has those logs and the recorded digest matches — the sync was fine, no resync.
	logs := logsWith([2]uint{0, 0})
	require.NoError(t, h.storage.SaveBlockLogDigest(nil, 12, executionclient.BlockLogsDigest(logs)))
	require.NoError(t, h.storage.SaveUnverifiedRange(nil, operatorstorage.UnverifiedRange{From: 12, To: 12, Cursor: 12}))
	h.exec.EXPECT().VerifyLogs(gomock.Any(), uint64(12), uint64(12)).Return(nil, nil)
	h.expectReceipts(12, logs)

	require.NoError(t, h.es.Verify(t.Context()))

	// No resync, and the range is retired clean (digest dropped).
	resync, err := h.storage.IsResyncRequired(nil)
	require.NoError(t, err)
	require.False(t, resync)

	ranges, err := h.storage.ListUnverifiedRanges(nil)
	require.NoError(t, err)
	require.Empty(t, ranges)

	_, found, err := h.storage.GetBlockLogDigest(nil, 12)
	require.NoError(t, err)
	require.False(t, found)
}

func TestVerify_ParksWhenReceiptsUnavailable(t *testing.T) {
	h := newVerifierHarness(t)

	// A block disagrees, but the EL doesn't support eth_getBlockReceipts, so there's no
	// authoritative source: don't resync, park the range (leave it pending and visible).
	require.NoError(t, h.storage.SaveUnverifiedRange(nil, operatorstorage.UnverifiedRange{From: 15, To: 15, Cursor: 15}))
	h.exec.EXPECT().VerifyLogs(gomock.Any(), uint64(15), uint64(15)).Return([]executionclient.BlockLogs{
		{BlockNumber: 15, Logs: logsWith([2]uint{0, 0})},
	}, nil)
	h.expectNoReceipts(15)

	require.NoError(t, h.es.Verify(t.Context()))

	// No resync flagged, and the range stays pending (parked) rather than being retired.
	resync, err := h.storage.IsResyncRequired(nil)
	require.NoError(t, err)
	require.False(t, resync)

	ranges, err := h.storage.ListUnverifiedRanges(nil)
	require.NoError(t, err)
	require.Len(t, ranges, 1)
}

func TestVerify_RateLimitSuppressesResync(t *testing.T) {
	h := newVerifierHarness(t)

	// A resync was flagged 1 minute ago (within the cooldown). A fresh confirmed miss is suppressed:
	// no new resync is flagged and the range is parked, so the node doesn't fatal+wipe in a loop.
	require.NoError(t, h.storage.SetLastResyncTime(nil, time.Now().Add(-time.Minute)))
	logs := logsWith([2]uint{0, 0})
	require.NoError(t, h.storage.SaveUnverifiedRange(nil, operatorstorage.UnverifiedRange{From: 15, To: 15, Cursor: 15}))
	h.exec.EXPECT().VerifyLogs(gomock.Any(), uint64(15), uint64(15)).Return([]executionclient.BlockLogs{
		{BlockNumber: 15, Logs: logs},
	}, nil)
	h.expectReceipts(15, logs)

	require.NoError(t, h.es.Verify(t.Context()))

	resync, err := h.storage.IsResyncRequired(nil)
	require.NoError(t, err)
	require.False(t, resync, "resync should be suppressed within the cooldown")

	ranges, err := h.storage.ListUnverifiedRanges(nil)
	require.NoError(t, err)
	require.Len(t, ranges, 1, "the range stays pending")
}

func TestVerify_ResumesFromCursor(t *testing.T) {
	h := newVerifierHarness(t)

	// Cursor at 20 means blocks 10-19 were already verified in a prior run; verification must
	// resume at the cursor, not re-check from the range start.
	require.NoError(t, h.storage.SaveUnverifiedRange(nil, operatorstorage.UnverifiedRange{From: 10, To: 30, Cursor: 20}))
	h.exec.EXPECT().VerifyLogs(gomock.Any(), uint64(20), uint64(30)).Return(nil, nil)

	require.NoError(t, h.es.Verify(t.Context()))

	ranges, err := h.storage.ListUnverifiedRanges(nil)
	require.NoError(t, err)
	require.Empty(t, ranges)
}

func TestVerify_ChunksRange(t *testing.T) {
	h := newVerifierHarness(t)
	h.es.verifyChunkSize = 5

	// A range wider than the chunk size is verified in successive chunks.
	require.NoError(t, h.storage.SaveUnverifiedRange(nil, operatorstorage.UnverifiedRange{From: 10, To: 20, Cursor: 10}))
	gomock.InOrder(
		h.exec.EXPECT().VerifyLogs(gomock.Any(), uint64(10), uint64(14)).Return(nil, nil),
		h.exec.EXPECT().VerifyLogs(gomock.Any(), uint64(15), uint64(19)).Return(nil, nil),
		h.exec.EXPECT().VerifyLogs(gomock.Any(), uint64(20), uint64(20)).Return(nil, nil),
	)

	require.NoError(t, h.es.Verify(t.Context()))

	ranges, err := h.storage.ListUnverifiedRanges(nil)
	require.NoError(t, err)
	require.Empty(t, ranges)
}

func TestVerifyWithRetry_RetriesTransientThenSucceeds(t *testing.T) {
	h := newVerifierHarness(t)
	h.es.verifyRetryInitialDelay = time.Millisecond
	h.es.verifyRetryMaxDelay = time.Millisecond

	require.NoError(t, h.storage.SaveUnverifiedRange(nil, operatorstorage.UnverifiedRange{From: 10, To: 10, Cursor: 10}))

	// A transient failure is retried in-process; the second pass verifies the range clean.
	gomock.InOrder(
		h.exec.EXPECT().VerifyLogs(gomock.Any(), uint64(10), uint64(10)).Return(nil, errors.New("transient EL error")),
		h.exec.EXPECT().VerifyLogs(gomock.Any(), uint64(10), uint64(10)).Return(nil, nil),
	)

	require.NoError(t, h.es.VerifyWithRetry(t.Context()))

	ranges, err := h.storage.ListUnverifiedRanges(nil)
	require.NoError(t, err)
	require.Empty(t, ranges)
}

func TestVerifyWithRetry_DoesNotRetryResyncRequired(t *testing.T) {
	h := newVerifierHarness(t)
	h.es.verifyRetryInitialDelay = time.Millisecond
	h.es.verifyRetryMaxDelay = time.Millisecond

	logs := logsWith([2]uint{0, 0})
	require.NoError(t, h.storage.SaveUnverifiedRange(nil, operatorstorage.UnverifiedRange{From: 15, To: 15, Cursor: 15}))
	// A confirmed miss is terminal: VerifyLogs is called exactly once (Times(1)), no retry.
	h.exec.EXPECT().VerifyLogs(gomock.Any(), uint64(15), uint64(15)).Return([]executionclient.BlockLogs{
		{BlockNumber: 15, Logs: logs},
	}, nil).Times(1)
	h.expectReceipts(15, logs)

	require.ErrorIs(t, h.es.VerifyWithRetry(t.Context()), ErrResyncRequired)
}

func TestVerifyWithRetry_StopsOnContextCancel(t *testing.T) {
	h := newVerifierHarness(t)
	h.es.verifyRetryInitialDelay = time.Hour // long enough that only cancellation ends the wait
	h.es.verifyRetryMaxDelay = time.Hour

	require.NoError(t, h.storage.SaveUnverifiedRange(nil, operatorstorage.UnverifiedRange{From: 10, To: 10, Cursor: 10}))

	ctx, cancel := context.WithCancel(t.Context())
	// Fail transiently and cancel, so the backoff wait exits instead of retrying.
	h.exec.EXPECT().VerifyLogs(gomock.Any(), uint64(10), uint64(10)).DoAndReturn(
		func(context.Context, uint64, uint64) ([]executionclient.BlockLogs, error) {
			cancel()
			return nil, errors.New("transient")
		})

	require.ErrorIs(t, h.es.VerifyWithRetry(ctx), context.Canceled)
}
