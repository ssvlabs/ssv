package dryrun

import (
	"context"
	"encoding/hex"
	"math"
	"math/big"
	"sort"
	"sync"
	"time"

	builderspec "github.com/attestantio/go-builder-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/mev/builderendpoint"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
)

type executionHeaderProvider interface {
	HeaderByNumber(ctx context.Context, blockNumber *big.Int) (*ethtypes.Header, error)
}

type builderHeaderProvider interface {
	GetHeader(ctx context.Context, mode string, slot phase0.Slot, parentHash phase0.Hash32, pubkey phase0.BLSPubKey) (*builderspec.VersionedSignedBuilderBid, builderendpoint.GetHeaderReport, error)
}

type Service struct {
	logger *zap.Logger

	exec              executionHeaderProvider
	builder           builderHeaderProvider
	parentHashTimeout time.Duration

	maxComparisons int

	mu          sync.Mutex
	comparisons []runner.MEVDryRunComparison

	reporterOnce sync.Once
	lastReportAt time.Time
}

func New(logger *zap.Logger, exec executionHeaderProvider, builder builderHeaderProvider, parentHashTimeout time.Duration) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}
	if parentHashTimeout <= 0 {
		parentHashTimeout = 150 * time.Millisecond
	}
	return &Service{
		logger:            logger.Named("MEVDryRun"),
		exec:              exec,
		builder:           builder,
		parentHashTimeout: parentHashTimeout,
		maxComparisons:    2048,
		lastReportAt:      time.Now(),
	}
}

func fillGetHeaderResult(res *runner.MEVShadowGetHeaderResult, bid *builderspec.VersionedSignedBuilderBid, rep builderendpoint.GetHeaderReport, err error) {
	if res == nil {
		return
	}
	if err != nil {
		res.Result = runner.MEVShadowResultError
		res.Cache = rep.Cache
		return
	}

	res.Cache = rep.Cache
	res.RelayHost = rep.RelayHost

	switch rep.Result {
	case runner.MEVShadowResultBid, runner.MEVShadowResultNoBid, runner.MEVShadowResultError:
		res.Result = rep.Result
	default:
		res.Result = runner.MEVShadowResultError
	}

	if rep.Result == runner.MEVShadowResultBid && bid != nil {
		if eth, ok := bidValueETH(bid); ok {
			res.ValueETH = eth
		}
	}
}

// StartReporter periodically logs a compact dry-run summary for the most recent comparisons window.
func (s *Service) StartReporter(ctx context.Context, interval time.Duration) {
	if s == nil || s.logger == nil || interval <= 0 {
		return
	}
	s.reporterOnce.Do(func() {
		ticker := time.NewTicker(interval)
		go func() {
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case now := <-ticker.C:
					s.logSummary(now)
				}
			}
		}()
	})
}

func (s *Service) StartShadowGetHeader(ctx context.Context, slot phase0.Slot, pubkey phase0.BLSPubKey) <-chan runner.MEVShadowGetHeaderResult {
	ch := make(chan runner.MEVShadowGetHeaderResult, 1)
	if s == nil || s.builder == nil || s.exec == nil {
		close(ch)
		return ch
	}
	if ctx == nil {
		ctx = context.Background()
	}

	startedAt := time.Now()
	go func() {
		defer close(ch)

		res := runner.MEVShadowGetHeaderResult{
			StartedAt: startedAt,
			Result:    runner.MEVShadowResultError,
		}

		// Fetch parent_hash from EL head at the moment of the baseline GetBeaconBlock call.
		pctx, cancel := context.WithTimeout(ctx, s.parentHashTimeout)
		headStart := time.Now()
		header, err := s.exec.HeaderByNumber(pctx, (*big.Int)(nil))
		res.HeadHashTook = time.Since(headStart)
		cancel()
		if err != nil || header == nil {
			res.Result = runner.MEVShadowResultHeadErr
			res.Took = 0
			ch <- res
			return
		}

		var parentHash phase0.Hash32
		h := header.Hash()
		copy(parentHash[:], h[:])
		res.ParentHashHex = hex.EncodeToString(parentHash[:])

		getHeaderStart := time.Now()
		bid, rep, err := s.builder.GetHeader(ctx, "dry_run", slot, parentHash, pubkey)
		res.Took = time.Since(getHeaderStart)
		fillGetHeaderResult(&res, bid, rep, err)

		ch <- res
	}()

	return ch
}

func (s *Service) StartShadowGetHeaderWithParentHash(ctx context.Context, slot phase0.Slot, parentHash phase0.Hash32, pubkey phase0.BLSPubKey) <-chan runner.MEVShadowGetHeaderResult {
	ch := make(chan runner.MEVShadowGetHeaderResult, 1)
	if s == nil || s.builder == nil {
		close(ch)
		return ch
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if parentHash == (phase0.Hash32{}) {
		close(ch)
		return ch
	}

	startedAt := time.Now()
	go func() {
		defer close(ch)

		res := runner.MEVShadowGetHeaderResult{
			StartedAt:     startedAt,
			ParentHashHex: hex.EncodeToString(parentHash[:]),
			Result:        runner.MEVShadowResultError,
		}

		getHeaderStart := time.Now()
		bid, rep, err := s.builder.GetHeader(ctx, "dry_run_exact_parent", slot, parentHash, pubkey)
		res.Took = time.Since(getHeaderStart)
		fillGetHeaderResult(&res, bid, rep, err)

		ch <- res
	}()

	return ch
}

func (s *Service) RecordComparison(_ context.Context, c runner.MEVDryRunComparison) {
	if s == nil || s.maxComparisons <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.comparisons == nil {
		capacity := 128
		if s.maxComparisons < capacity {
			capacity = s.maxComparisons
		}
		s.comparisons = make([]runner.MEVDryRunComparison, 0, capacity)
	}

	s.comparisons = append(s.comparisons, c)
	if len(s.comparisons) > s.maxComparisons {
		trimmed := make([]runner.MEVDryRunComparison, s.maxComparisons)
		copy(trimmed, s.comparisons[len(s.comparisons)-s.maxComparisons:])
		s.comparisons = trimmed
	}
}

func (s *Service) Comparisons(limit int) []runner.MEVDryRunComparison {
	if s == nil {
		return nil
	}
	if limit <= 0 {
		limit = 100
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.comparisons) == 0 {
		return nil
	}
	if limit > len(s.comparisons) {
		limit = len(s.comparisons)
	}

	out := make([]runner.MEVDryRunComparison, 0, limit)
	for i := len(s.comparisons) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.comparisons[i])
	}
	return out
}

func (s *Service) logSummary(now time.Time) {
	// Build a windowed snapshot based on baseline start time.
	s.mu.Lock()
	from := s.lastReportAt
	s.lastReportAt = now
	snapshot := append([]runner.MEVDryRunComparison(nil), s.comparisons...)
	s.mu.Unlock()

	type counts struct {
		total int

		baselineOK  int
		baselineErr int

		shadowBid     int
		shadowNoBid   int
		shadowErr     int
		shadowHeadErr int
		shadowTimeout int

		parentHashKnown    int
		parentHashMatch    int
		parentHashMismatch int

		exactTotal   int
		exactBid     int
		exactNoBid   int
		exactErr     int
		exactTimeout int

		recoveredBid int
	}
	var c counts
	baselineTook := make([]time.Duration, 0, 256)
	shadowTotalTook := make([]time.Duration, 0, 256)
	headHashTook := make([]time.Duration, 0, 256)
	shadowMinusBaseline := make([]time.Duration, 0, 256)
	exactTook := make([]time.Duration, 0, 256)

	for _, cmp := range snapshot {
		if cmp.Baseline.StartedAt.Before(from) || cmp.Baseline.StartedAt.After(now) {
			continue
		}
		c.total++
		if cmp.Baseline.Result == runner.MEVBaselineResultOK {
			c.baselineOK++
		} else {
			c.baselineErr++
		}

		switch cmp.Shadow.Result {
		case runner.MEVShadowResultBid:
			c.shadowBid++
		case runner.MEVShadowResultNoBid:
			c.shadowNoBid++
		case runner.MEVShadowResultError:
			c.shadowErr++
		case runner.MEVShadowResultHeadErr:
			c.shadowHeadErr++
		case runner.MEVShadowResultTimeout:
			c.shadowTimeout++
		default:
			c.shadowErr++
		}

		if cmp.BaselineExecParentHash != "" && cmp.Shadow.ParentHashHex != "" {
			c.parentHashKnown++
			if cmp.ParentHashMatch {
				c.parentHashMatch++
			} else {
				c.parentHashMismatch++
			}
		}

		if cmp.ShadowExactParent != nil {
			c.exactTotal++
			switch cmp.ShadowExactParent.Result {
			case runner.MEVShadowResultBid:
				c.exactBid++
			case runner.MEVShadowResultNoBid:
				c.exactNoBid++
			case runner.MEVShadowResultTimeout:
				c.exactTimeout++
			case runner.MEVShadowResultError, runner.MEVShadowResultHeadErr:
				c.exactErr++
			default:
				c.exactErr++
			}
			if cmp.ShadowExactParent.Took > 0 {
				exactTook = append(exactTook, cmp.ShadowExactParent.Took)
			}
		}
		if cmp.RecoveredBid {
			c.recoveredBid++
		}

		if cmp.Baseline.Took > 0 {
			baselineTook = append(baselineTook, cmp.Baseline.Took)
		}
		if cmp.Shadow.HeadHashTook > 0 {
			headHashTook = append(headHashTook, cmp.Shadow.HeadHashTook)
		}
		st := cmp.Shadow.HeadHashTook + cmp.Shadow.Took
		if st > 0 {
			shadowTotalTook = append(shadowTotalTook, st)
		}
		if cmp.Baseline.Took > 0 && st > 0 {
			shadowMinusBaseline = append(shadowMinusBaseline, st-cmp.Baseline.Took)
		}
	}

	if c.total == 0 {
		return
	}

	p95 := func(arr []time.Duration) time.Duration {
		if len(arr) == 0 {
			return 0
		}
		sort.Slice(arr, func(i, j int) bool { return arr[i] < arr[j] })
		i := int(math.Ceil(float64(len(arr))*0.95)) - 1
		if i < 0 {
			i = 0
		}
		if i >= len(arr) {
			i = len(arr) - 1
		}
		return arr[i]
	}

	s.logger.Info(
		"mev dry-run hourly summary",
		zap.Time("window_from", from),
		zap.Time("window_to", now),
		zap.Int("window_total", c.total),
		zap.Int("baseline_ok", c.baselineOK),
		zap.Int("baseline_error", c.baselineErr),
		zap.Int("shadow_bid", c.shadowBid),
		zap.Int("shadow_no_bid", c.shadowNoBid),
		zap.Int("shadow_error", c.shadowErr),
		zap.Int("shadow_head_error", c.shadowHeadErr),
		zap.Int("shadow_timeout", c.shadowTimeout),
		zap.Int("parent_hash_known", c.parentHashKnown),
		zap.Int("parent_hash_match", c.parentHashMatch),
		zap.Int("parent_hash_mismatch", c.parentHashMismatch),
		zap.Int("shadow_exact_total", c.exactTotal),
		zap.Int("shadow_exact_bid", c.exactBid),
		zap.Int("shadow_exact_no_bid", c.exactNoBid),
		zap.Int("shadow_exact_error", c.exactErr),
		zap.Int("shadow_exact_timeout", c.exactTimeout),
		zap.Int("recovered_bid", c.recoveredBid),
		zap.Duration("p95_baseline_get_block", p95(baselineTook)),
		zap.Duration("p95_shadow_head_hash", p95(headHashTook)),
		zap.Duration("p95_shadow_total", p95(shadowTotalTook)),
		zap.Duration("p95_shadow_minus_baseline", p95(shadowMinusBaseline)),
		zap.Duration("p95_shadow_exact_parent", p95(exactTook)),
	)
}

func bidValueETH(bid *builderspec.VersionedSignedBuilderBid) (float64, bool) {
	if bid == nil {
		return 0, false
	}
	valueWei, err := bid.Value()
	if err != nil || valueWei == nil {
		return 0, false
	}
	weiAsFloat := new(big.Float).SetInt(valueWei.ToBig())
	ethAsFloat := new(big.Float).Quo(weiAsFloat, big.NewFloat(1e18))
	eth, _ := ethAsFloat.Float64()
	if math.IsNaN(eth) || math.IsInf(eth, 0) {
		return 0, false
	}
	return eth, true
}
