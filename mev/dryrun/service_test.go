package dryrun

import (
	"context"
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	builderspec "github.com/attestantio/go-builder-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/mev/builderendpoint"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
)

type stubExec struct {
	header *ethtypes.Header
	err    error
}

func (s *stubExec) HeaderByNumber(_ context.Context, _ *big.Int) (*ethtypes.Header, error) {
	return s.header, s.err
}

type builderCall struct {
	mode       string
	slot       phase0.Slot
	parentHash phase0.Hash32
	pubkey     phase0.BLSPubKey
}

type stubBuilder struct {
	calls []builderCall
	bid   *builderspec.VersionedSignedBuilderBid
	rep   builderendpoint.GetHeaderReport
	err   error
}

func (s *stubBuilder) GetHeader(_ context.Context, mode string, slot phase0.Slot, parentHash phase0.Hash32, pubkey phase0.BLSPubKey) (*builderspec.VersionedSignedBuilderBid, builderendpoint.GetHeaderReport, error) {
	s.calls = append(s.calls, builderCall{
		mode:       mode,
		slot:       slot,
		parentHash: parentHash,
		pubkey:     pubkey,
	})
	return s.bid, s.rep, s.err
}

func TestService_StartShadowGetHeader_Success(t *testing.T) {
	t.Parallel()

	header := &ethtypes.Header{
		Number: big.NewInt(123),
		Time:   1,
		Extra:  []byte("dryrun"),
	}
	h := header.Hash()
	var expectedParentHash phase0.Hash32
	copy(expectedParentHash[:], h[:])

	exec := &stubExec{header: header}
	builder := &stubBuilder{
		rep: builderendpoint.GetHeaderReport{
			Result:    "no_bid",
			Cache:     "miss",
			RelayHost: "relay.example",
		},
	}
	svc := New(zap.NewNop(), exec, builder, 50*time.Millisecond)

	var pubkey phase0.BLSPubKey
	pubkey[0] = 1
	ch := svc.StartShadowGetHeader(context.Background(), phase0.Slot(10), pubkey)
	res, ok := <-ch
	require.True(t, ok)
	_, stillOpen := <-ch
	require.False(t, stillOpen)

	require.Equal(t, "no_bid", res.Result)
	require.Equal(t, "miss", res.Cache)
	require.Equal(t, "relay.example", res.RelayHost)
	require.Equal(t, hex.EncodeToString(expectedParentHash[:]), res.ParentHashHex)

	require.Len(t, builder.calls, 1)
	require.Equal(t, "dry_run", builder.calls[0].mode)
	require.Equal(t, phase0.Slot(10), builder.calls[0].slot)
	require.Equal(t, expectedParentHash, builder.calls[0].parentHash)
	require.Equal(t, pubkey, builder.calls[0].pubkey)
}

func TestService_StartShadowGetHeader_HeadError(t *testing.T) {
	t.Parallel()

	exec := &stubExec{err: context.DeadlineExceeded}
	builder := &stubBuilder{}
	svc := New(zap.NewNop(), exec, builder, 10*time.Millisecond)

	ch := svc.StartShadowGetHeader(context.Background(), phase0.Slot(10), phase0.BLSPubKey{})
	res, ok := <-ch
	require.True(t, ok)
	require.Equal(t, "head_error", res.Result)
	require.Zero(t, res.Took)
	require.Empty(t, res.ParentHashHex)
	require.Len(t, builder.calls, 0)
}

func TestService_StartShadowGetHeaderWithParentHash_ZeroParentCloses(t *testing.T) {
	t.Parallel()

	svc := &Service{}
	ch := svc.StartShadowGetHeaderWithParentHash(context.Background(), phase0.Slot(1), phase0.Hash32{}, phase0.BLSPubKey{})
	_, ok := <-ch
	require.False(t, ok)
}

func TestService_StartShadowGetHeaderWithParentHash_CallsBuilder(t *testing.T) {
	t.Parallel()

	builder := &stubBuilder{
		rep: builderendpoint.GetHeaderReport{Result: "bid", Cache: "hit", RelayHost: "relay.example"},
	}
	svc := New(zap.NewNop(), nil, builder, 10*time.Millisecond)

	var parent phase0.Hash32
	parent[0] = 0xaa
	ch := svc.StartShadowGetHeaderWithParentHash(context.Background(), phase0.Slot(2), parent, phase0.BLSPubKey{})
	res, ok := <-ch
	require.True(t, ok)

	require.Equal(t, "bid", res.Result)
	require.Equal(t, "hit", res.Cache)
	require.Equal(t, "relay.example", res.RelayHost)
	require.Equal(t, hex.EncodeToString(parent[:]), res.ParentHashHex)

	require.Len(t, builder.calls, 1)
	require.Equal(t, "dry_run_exact_parent", builder.calls[0].mode)
	require.Equal(t, phase0.Slot(2), builder.calls[0].slot)
	require.Equal(t, parent, builder.calls[0].parentHash)
}

func TestService_RecordComparison_TrimsAndReturnsNewestFirst(t *testing.T) {
	t.Parallel()

	svc := &Service{maxComparisons: 2}
	svc.RecordComparison(context.Background(), runner.MEVDryRunComparison{Slot: phase0.Slot(1)})
	svc.RecordComparison(context.Background(), runner.MEVDryRunComparison{Slot: phase0.Slot(2)})
	svc.RecordComparison(context.Background(), runner.MEVDryRunComparison{Slot: phase0.Slot(3)})

	got := svc.Comparisons(10)
	require.Len(t, got, 2)
	require.Equal(t, phase0.Slot(3), got[0].Slot)
	require.Equal(t, phase0.Slot(2), got[1].Slot)

	got1 := svc.Comparisons(1)
	require.Len(t, got1, 1)
	require.Equal(t, phase0.Slot(3), got1[0].Slot)
}

func TestService_LogSummary_EmitsExpectedCounts(t *testing.T) {
	t.Parallel()

	core, logs := observer.New(zap.InfoLevel)
	logger := zap.New(core)

	from := time.Unix(1000, 0)
	now := from.Add(time.Hour)

	cmp1 := runner.MEVDryRunComparison{
		BaselineExecParentHash: "aa",
		ParentHashMatch:        true,
		Baseline: runner.MEVBaselineGetBlockResult{
			StartedAt: from.Add(10 * time.Second),
			Took:      100 * time.Millisecond,
			Result:    "ok",
		},
		Shadow: runner.MEVShadowGetHeaderResult{
			Result:        "bid",
			ParentHashHex: "aa",
			Took:          50 * time.Millisecond,
		},
	}
	exact := runner.MEVShadowGetHeaderResult{Result: "bid", Took: 25 * time.Millisecond}
	cmp2 := runner.MEVDryRunComparison{
		BaselineExecParentHash: "bb",
		ParentHashMatch:        false,
		Baseline: runner.MEVBaselineGetBlockResult{
			StartedAt: from.Add(20 * time.Second),
			Took:      120 * time.Millisecond,
			Result:    "ok",
		},
		Shadow: runner.MEVShadowGetHeaderResult{
			Result:        "no_bid",
			ParentHashHex: "cc",
			Took:          60 * time.Millisecond,
		},
		ShadowExactParent: &exact,
		RecoveredBid:      true,
	}

	svc := &Service{
		logger:         logger,
		maxComparisons: 10,
		lastReportAt:   from,
		comparisons: []runner.MEVDryRunComparison{
			cmp1,
			cmp2,
		},
	}

	svc.logSummary(now)

	entries := logs.All()
	require.Len(t, entries, 1)
	require.Equal(t, "mev dry-run hourly summary", entries[0].Message)

	ctx := entries[0].ContextMap()
	require.EqualValues(t, 2, ctx["window_total"])
	require.EqualValues(t, 2, ctx["baseline_ok"])
	require.EqualValues(t, 0, ctx["baseline_error"])
	require.EqualValues(t, 1, ctx["shadow_bid"])
	require.EqualValues(t, 1, ctx["shadow_no_bid"])
	require.EqualValues(t, 0, ctx["shadow_error"])
	require.EqualValues(t, 0, ctx["shadow_head_error"])
	require.EqualValues(t, 0, ctx["shadow_timeout"])
	require.EqualValues(t, 2, ctx["parent_hash_known"])
	require.EqualValues(t, 1, ctx["parent_hash_match"])
	require.EqualValues(t, 1, ctx["parent_hash_mismatch"])
	require.EqualValues(t, 1, ctx["shadow_exact_total"])
	require.EqualValues(t, 1, ctx["shadow_exact_bid"])
	require.EqualValues(t, 0, ctx["shadow_exact_no_bid"])
	require.EqualValues(t, 0, ctx["shadow_exact_error"])
	require.EqualValues(t, 0, ctx["shadow_exact_timeout"])
	require.EqualValues(t, 1, ctx["recovered_bid"])
}
