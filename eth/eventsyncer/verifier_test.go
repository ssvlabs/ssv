package eventsyncer

import (
	"context"
	"errors"
	"testing"
	"time"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/networkconfig"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// verifierHarness wires the syncer's background verifier to a mocked execution client (so we
// control the authoritative logs) and real in-memory storage (so log IDs and ranges round-trip).
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

// record stores the log IDs an optimistic sync would have recorded for a block.
func (h *verifierHarness) record(t *testing.T, block uint64, logs []ethtypes.Log) {
	ids := executionclient.EncodeLogIDs(executionclient.BlockLogIDs(logs))
	require.NoError(t, h.storage.SaveBlockLogIDs(nil, block, ids))
}

// expectReceipts mocks the authoritative (receipts-derived) logs for a block.
func (h *verifierHarness) expectReceipts(block uint64, logs []ethtypes.Log) *gomock.Call {
	return h.exec.EXPECT().BlockContractLogs(gomock.Any(), block).Return(logs, true, nil)
}

// expectNoReceipts mocks no client being able to serve a block's receipts.
func (h *verifierHarness) expectNoReceipts(block uint64) *gomock.Call {
	return h.exec.EXPECT().BlockContractLogs(gomock.Any(), block).Return(nil, false, nil)
}

// requireState asserts what verification left behind: pending ranges, unsettled blocks, and
// whether a resync is flagged.
func (h *verifierHarness) requireState(t *testing.T, ranges []operatorstorage.UnverifiedRange, unsettled []uint64, resync bool) {
	t.Helper()

	gotRanges, err := h.storage.ListUnverifiedRanges(nil)
	require.NoError(t, err)
	require.Equal(t, ranges, gotRanges, "pending ranges")

	gotUnsettled, err := h.storage.ListUnsettledBlocks(nil)
	require.NoError(t, err)
	require.Equal(t, unsettled, gotUnsettled, "unsettled blocks")

	gotResync, err := h.storage.IsResyncRequired(nil)
	require.NoError(t, err)
	require.Equal(t, resync, gotResync, "resync flagged")
}

func (h *verifierHarness) requireRecorded(t *testing.T, block uint64, want bool) {
	t.Helper()
	_, found, err := h.storage.GetBlockLogIDs(nil, block)
	require.NoError(t, err)
	require.Equal(t, want, found, "recorded log IDs for block %d", block)
}

func TestVerify_NoRanges(t *testing.T) {
	h := newVerifierHarness(t)
	// Nothing journalled: Verify is a no-op and must not touch the execution client.
	require.NoError(t, h.es.Verify(t.Context()))
}

func TestVerify_CleanRange(t *testing.T) {
	h := newVerifierHarness(t)

	logs12 := logsWith([2]uint{0, 0})
	logs15 := logsWith([2]uint{0, 0}, [2]uint{1, 1})
	h.record(t, 12, logs12)
	h.record(t, 15, logs15)
	require.NoError(t, h.storage.SaveUnverifiedRange(nil, operatorstorage.UnverifiedRange{From: 10, To: 20, Cursor: 10}))

	// The verify-time fetch matches what the sync recorded, so the range is clean.
	h.exec.EXPECT().VerifyLogs(gomock.Any(), uint64(10), uint64(20)).Return([]executionclient.BlockLogs{
		{BlockNumber: 12, Logs: logs12},
		{BlockNumber: 15, Logs: logs15},
	}, nil)

	require.NoError(t, h.es.Verify(t.Context()))

	// A clean range is removed along with its recorded log IDs.
	h.requireState(t, nil, nil, false)
	h.requireRecorded(t, 12, false)
	h.requireRecorded(t, 15, false)
}

func TestVerify_ReceiptsConfirmMissingLogs(t *testing.T) {
	h := newVerifierHarness(t)

	// The sync recorded nothing for block 15, the verify-time fetch shows contract logs there, and
	// receipts confirm them — the silent miss #2990 is about. Resync.
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

	// The sync recorded one log, the verify-time fetch shows two, and receipts confirm two: the
	// recorded response was an incomplete subset. Resync.
	recorded := logsWith([2]uint{0, 0})
	truth := logsWith([2]uint{0, 0}, [2]uint{1, 1})
	h.record(t, 12, recorded)
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

	// The sync recorded logs for block 12, but the verify-time fetch returns it empty (a different
	// EL under MultiClient, say). Receipts confirm the block really has those logs and they match
	// the recorded IDs — the sync was fine, no resync.
	logs := logsWith([2]uint{0, 0})
	h.record(t, 12, logs)
	require.NoError(t, h.storage.SaveUnverifiedRange(nil, operatorstorage.UnverifiedRange{From: 12, To: 12, Cursor: 12}))
	h.exec.EXPECT().VerifyLogs(gomock.Any(), uint64(12), uint64(12)).Return(nil, nil)
	h.expectReceipts(12, logs)

	require.NoError(t, h.es.Verify(t.Context()))

	h.requireState(t, nil, nil, false)
	h.requireRecorded(t, 12, false)
}

func TestVerify_UnsettledWhenReceiptsUnavailable(t *testing.T) {
	h := newVerifierHarness(t)

	// Block 15 disagrees with what the sync recorded, but no client can serve its receipts, so
	// there's no authoritative source: no resync, no claim of verification. The block is recorded
	// unsettled and the range moves on.
	require.NoError(t, h.storage.SaveUnverifiedRange(nil, operatorstorage.UnverifiedRange{From: 10, To: 20, Cursor: 10}))
	h.exec.EXPECT().VerifyLogs(gomock.Any(), uint64(10), uint64(20)).Return([]executionclient.BlockLogs{
		{BlockNumber: 15, Logs: logsWith([2]uint{0, 0})},
	}, nil)
	h.expectNoReceipts(15)

	require.NoError(t, h.es.Verify(t.Context()))

	h.requireState(t, nil, []uint64{15}, false)
}

func TestVerify_UnsettledSuspectIsNotRetiredAsVerified(t *testing.T) {
	h := newVerifierHarness(t)

	// The verify-time fetch couldn't settle block 15 either (a bloom match with no logs and no
	// receipts from any client), so "nothing recorded, nothing fetched" proves nothing there: the
	// block stays unsettled, keeping whatever the sync recorded. VerifyLogs already asked every
	// client for receipts, so the verifier doesn't ask again.
	logs := logsWith([2]uint{0, 0})
	h.record(t, 15, logs)
	require.NoError(t, h.storage.SaveUnverifiedRange(nil, operatorstorage.UnverifiedRange{From: 10, To: 20, Cursor: 10}))
	h.exec.EXPECT().VerifyLogs(gomock.Any(), uint64(10), uint64(20)).Return([]executionclient.BlockLogs{
		{BlockNumber: 15, Unsettled: true},
	}, nil)

	require.NoError(t, h.es.Verify(t.Context()))

	h.requireState(t, nil, []uint64{15}, false)
	h.requireRecorded(t, 15, true)
}

func TestVerify_InconsistentReceiptsStayUnsettled(t *testing.T) {
	h := newVerifierHarness(t)

	// The receipts lack a log the sync recorded (a truncated response, or one numbering logs
	// differently). getLogs never invents logs, so it's the receipts that are inconsistent: the
	// block stays unsettled instead of triggering a false wipe.
	h.record(t, 12, logsWith([2]uint{0, 0}, [2]uint{0, 1}))
	require.NoError(t, h.storage.SaveUnverifiedRange(nil, operatorstorage.UnverifiedRange{From: 12, To: 12, Cursor: 12}))
	h.exec.EXPECT().VerifyLogs(gomock.Any(), uint64(12), uint64(12)).Return([]executionclient.BlockLogs{
		{BlockNumber: 12, Logs: logsWith([2]uint{0, 0})},
	}, nil)
	h.expectReceipts(12, logsWith([2]uint{0, 0}))

	require.NoError(t, h.es.Verify(t.Context()))

	h.requireState(t, nil, []uint64{12}, false)
	h.requireRecorded(t, 12, true)
}

func TestVerify_RateLimitedMissStaysUnsettled(t *testing.T) {
	h := newVerifierHarness(t)

	// A resync was flagged 1 minute ago (within the cooldown). A fresh confirmed miss is
	// suppressed: no new resync is flagged and the block stays unsettled, so the node doesn't
	// fatal+wipe in a loop.
	require.NoError(t, h.storage.SetLastResyncTime(nil, time.Now().Add(-time.Minute)))
	logs := logsWith([2]uint{0, 0})
	require.NoError(t, h.storage.SaveUnverifiedRange(nil, operatorstorage.UnverifiedRange{From: 15, To: 15, Cursor: 15}))
	h.exec.EXPECT().VerifyLogs(gomock.Any(), uint64(15), uint64(15)).Return([]executionclient.BlockLogs{
		{BlockNumber: 15, Logs: logs},
	}, nil)
	h.expectReceipts(15, logs)

	require.NoError(t, h.es.Verify(t.Context()))

	h.requireState(t, nil, []uint64{15}, false)
}

func TestVerify_ResumesFromCursor(t *testing.T) {
	h := newVerifierHarness(t)

	// Cursor at 20 means blocks 10-19 were already verified in a prior run; verification must
	// resume at the cursor, not re-check from the range start.
	require.NoError(t, h.storage.SaveUnverifiedRange(nil, operatorstorage.UnverifiedRange{From: 10, To: 30, Cursor: 20}))
	h.exec.EXPECT().VerifyLogs(gomock.Any(), uint64(20), uint64(30)).Return(nil, nil)

	require.NoError(t, h.es.Verify(t.Context()))

	h.requireState(t, nil, nil, false)
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

	h.requireState(t, nil, nil, false)
}

func TestVerify_SettlesUnsettledBlocks(t *testing.T) {
	h := newVerifierHarness(t)

	// Receipts now serve unsettled block 15 and match what the sync recorded: it's retired.
	logs := logsWith([2]uint{0, 0})
	h.record(t, 15, logs)
	require.NoError(t, h.storage.SaveUnsettledBlock(nil, 15))
	h.expectReceipts(15, logs)

	require.NoError(t, h.es.Verify(t.Context()))

	h.requireState(t, nil, nil, false)
	h.requireRecorded(t, 15, false)
}

func TestVerify_SettlingConfirmsMiss(t *testing.T) {
	h := newVerifierHarness(t)

	// An inline sync processed block 15 as empty for lack of receipts; now receipts show logs there.
	require.NoError(t, h.storage.SaveUnsettledBlock(nil, 15))
	h.expectReceipts(15, logsWith([2]uint{0, 0}))

	require.ErrorIs(t, h.es.Verify(t.Context()), ErrResyncRequired)

	resync, err := h.storage.IsResyncRequired(nil)
	require.NoError(t, err)
	require.True(t, resync)
}

func TestVerify_UnansweredBlockDoesNotStarveOthers(t *testing.T) {
	h := newVerifierHarness(t)

	// No client can answer for block 15 (its receipts are pruned, say), but that's no reason to
	// stop: block 20's receipts show it was a bloom false positive, so it's retired.
	require.NoError(t, h.storage.SaveUnsettledBlock(nil, 15))
	require.NoError(t, h.storage.SaveUnsettledBlock(nil, 20))
	gomock.InOrder(
		h.expectNoReceipts(15),
		h.expectReceipts(20, nil),
	)

	require.NoError(t, h.es.Verify(t.Context()))

	h.requireState(t, nil, []uint64{15}, false)
}

func TestVerify_RechecksRotateWithinBudget(t *testing.T) {
	h := newVerifierHarness(t)
	h.es.verifyUnansweredPerPass = 2

	// With nothing answering, a pass stops after its budget of unanswered blocks, and the next one
	// resumes after the last block tried (wrapping around) rather than starting over.
	for _, block := range []uint64{15, 20, 25} {
		require.NoError(t, h.storage.SaveUnsettledBlock(nil, block))
	}
	gomock.InOrder(
		h.expectNoReceipts(15),
		h.expectNoReceipts(20),
		h.expectNoReceipts(25),
		h.expectNoReceipts(15),
	)

	require.NoError(t, h.es.Verify(t.Context()))
	require.NoError(t, h.es.Verify(t.Context()))

	h.requireState(t, nil, []uint64{15, 20, 25}, false)
}

func TestVerify_FailedLookupLeavesBlockUnsettled(t *testing.T) {
	h := newVerifierHarness(t)

	// Receipts lookups for blocks 12 and 15 fail (a dead client under MultiClient, say). Neither
	// stalls verification: unsettled block 15 stays unsettled, the range still gets verified, and
	// its disagreeing block 12 is recorded unsettled too.
	require.NoError(t, h.storage.SaveUnsettledBlock(nil, 15))
	h.record(t, 12, logsWith([2]uint{0, 0}))
	require.NoError(t, h.storage.SaveUnverifiedRange(nil, operatorstorage.UnverifiedRange{From: 10, To: 14, Cursor: 10}))
	gomock.InOrder(
		h.exec.EXPECT().BlockContractLogs(gomock.Any(), uint64(15)).Return(nil, false, errors.New("client down")),
		h.exec.EXPECT().VerifyLogs(gomock.Any(), uint64(10), uint64(14)).Return(nil, nil),
		h.exec.EXPECT().BlockContractLogs(gomock.Any(), uint64(12)).Return(nil, false, errors.New("client down")),
	)

	require.NoError(t, h.es.Verify(t.Context()))

	h.requireState(t, nil, []uint64{12, 15}, false)
}

func TestVerify_SettlingContinuesPastInconsistentReceipts(t *testing.T) {
	h := newVerifierHarness(t)

	// Receipts answer for block 15 but lack a recorded log, so it stays unsettled; the source is
	// up, so re-checking moves on and settles block 20.
	h.record(t, 15, logsWith([2]uint{0, 0}, [2]uint{0, 1}))
	h.record(t, 20, logsWith([2]uint{0, 0}))
	require.NoError(t, h.storage.SaveUnsettledBlock(nil, 15))
	require.NoError(t, h.storage.SaveUnsettledBlock(nil, 20))
	gomock.InOrder(
		h.expectReceipts(15, logsWith([2]uint{0, 0})),
		h.expectReceipts(20, logsWith([2]uint{0, 0})),
	)

	require.NoError(t, h.es.Verify(t.Context()))

	h.requireState(t, nil, []uint64{15}, false)
	h.requireRecorded(t, 20, false)
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

	require.NoError(t, h.es.verifyWithRetry(t.Context()))

	h.requireState(t, nil, nil, false)
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

	require.ErrorIs(t, h.es.verifyWithRetry(t.Context()), ErrResyncRequired)
}

func TestVerifyWithRetry_RechecksUnsettledUntilSettled(t *testing.T) {
	h := newVerifierHarness(t)
	h.es.verifyRetryInitialDelay = time.Millisecond
	h.es.verifyUnsettledRecheckMaxDelay = time.Millisecond

	// The first pass leaves block 15 unsettled: the verify-time fetch misses the logs the sync
	// recorded there, and receipts are unavailable. A later in-process re-check finds receipts
	// vindicating the recorded logs, so the block is retired — no resync, no restart.
	logs := logsWith([2]uint{0, 0})
	h.record(t, 15, logs)
	require.NoError(t, h.storage.SaveUnverifiedRange(nil, operatorstorage.UnverifiedRange{From: 10, To: 20, Cursor: 10}))
	gomock.InOrder(
		h.exec.EXPECT().VerifyLogs(gomock.Any(), uint64(10), uint64(20)).Return(nil, nil),
		h.expectNoReceipts(15),
		h.expectReceipts(15, logs),
	)

	require.NoError(t, h.es.verifyWithRetry(t.Context()))

	h.requireState(t, nil, nil, false)
	h.requireRecorded(t, 15, false)
}

func TestVerifyWithRetry_KeepsRecheckingUnsettled(t *testing.T) {
	h := newVerifierHarness(t)
	h.es.verifyRetryInitialDelay = time.Millisecond
	h.es.verifyUnsettledRecheckMaxDelay = time.Millisecond

	// A block that stays unsettled doesn't end verification (that would leave it unverified until
	// a restart): it's re-checked pass after pass until ctx is canceled.
	require.NoError(t, h.storage.SaveUnsettledBlock(nil, 15))

	ctx, cancel := context.WithCancel(t.Context())
	checks := 0
	h.exec.EXPECT().BlockContractLogs(gomock.Any(), uint64(15)).DoAndReturn(
		func(context.Context, uint64) ([]ethtypes.Log, bool, error) {
			checks++
			if checks == 3 {
				cancel()
			}
			return nil, false, nil
		}).Times(3)

	require.ErrorIs(t, h.es.verifyWithRetry(ctx), context.Canceled)
}

func TestVerifyWithRetry_FlagsSuppressedRepairOnceCooldownExpires(t *testing.T) {
	h := newVerifierHarness(t)
	h.es.verifyRetryInitialDelay = time.Millisecond
	h.es.verifyUnsettledRecheckMaxDelay = time.Millisecond

	// A confirmed miss within the cooldown of an earlier resync is suppressed and the block left
	// unsettled. Its re-check confirms the miss again and, the cooldown having expired by then,
	// flags the repair in-process instead of leaving it for a restart.
	require.NoError(t, h.storage.SetLastResyncTime(nil, time.Now().Add(-time.Minute)))
	require.NoError(t, h.storage.SaveUnsettledBlock(nil, 15))
	logs := logsWith([2]uint{0, 0})
	gomock.InOrder(
		h.expectReceipts(15, logs),
		h.exec.EXPECT().BlockContractLogs(gomock.Any(), uint64(15)).DoAndReturn(
			func(context.Context, uint64) ([]ethtypes.Log, bool, error) {
				// By the re-check, the earlier resync is older than the cooldown.
				require.NoError(t, h.storage.SetLastResyncTime(nil, time.Now().Add(-2*h.es.resyncCooldown)))
				return logs, true, nil
			}),
	)

	require.ErrorIs(t, h.es.verifyWithRetry(t.Context()), ErrResyncRequired)

	resync, err := h.storage.IsResyncRequired(nil)
	require.NoError(t, err)
	require.True(t, resync)
}

func TestRunVerifier_SettlesBlocksRecordedWhileIdle(t *testing.T) {
	h := newVerifierHarness(t)
	h.es.verifyRetryInitialDelay = time.Millisecond
	h.es.verifyUnsettledRecheckMaxDelay = time.Millisecond
	core, logs := observer.New(zap.InfoLevel)
	h.es.logger = zap.New(core)

	// The first pass verifies a clean range and goes idle; streaming then records a block it
	// couldn't settle. The idle verifier checks back and settles it (receipts show no contract
	// logs there), with no restart.
	require.NoError(t, h.storage.SaveUnverifiedRange(nil, operatorstorage.UnverifiedRange{From: 10, To: 10, Cursor: 10}))
	h.exec.EXPECT().VerifyLogs(gomock.Any(), uint64(10), uint64(10)).Return(nil, nil)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.es.RunVerifier(ctx) }()
	require.Eventually(t, func() bool {
		return logs.FilterMessageSnippet("background verification complete").Len() > 0
	}, 10*time.Second, time.Millisecond, "the first pass completes and the verifier goes idle")

	h.expectReceipts(15, nil)
	require.NoError(t, h.storage.SaveUnsettledBlock(nil, 15))
	require.Eventually(t, func() bool {
		unsettled, err := h.storage.ListUnsettledBlocks(nil)
		return err == nil && len(unsettled) == 0
	}, 10*time.Second, time.Millisecond, "the idle verifier settles the block")

	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
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

	require.ErrorIs(t, h.es.verifyWithRetry(ctx), context.Canceled)
}
