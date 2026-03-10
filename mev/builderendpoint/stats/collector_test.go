package stats

import (
	"testing"
	"time"
)

func TestCollectorSnapshotAndReset_ComputesHeadlineStats(t *testing.T) {
	t.Parallel()

	c := NewCollector(Options{})
	c.windowStart = time.Unix(0, 0)

	// getHeader bid: hit is fast, miss is slow.
	c.ObserveGetHeader("hit", "bid", 10*time.Millisecond, 200*time.Millisecond)
	c.ObserveGetHeader("hit", "bid", 10*time.Millisecond, 200*time.Millisecond)
	c.ObserveGetHeader("miss", "bid", 100*time.Millisecond, 1*time.Second)
	c.ObserveGetHeader("miss", "bid", 100*time.Millisecond, 1*time.Second)
	// include a no_bid to exercise rates.
	c.ObserveGetHeader("miss", "no_bid", 20*time.Millisecond, 500*time.Millisecond)

	// prefetch lead time
	c.ObservePrefetchLeadTime(500 * time.Millisecond)
	c.ObservePrefetchRequest()
	c.ObservePrefetchResult("cached")

	// value + relay winners
	c.ObserveWinningBid("get_header", "relay-a", 0.01)
	c.ObserveWinningBid("prefetch", "relay-a", 0.02)

	r := c.SnapshotAndReset(time.Unix(3600, 0))

	if r.WindowSeconds <= 0 {
		t.Fatalf("expected positive window seconds")
	}

	// p95 miss is 100ms, p95 hit is 10ms -> 90% improvement
	if r.GetHeader.P95MissMs < 99 || r.GetHeader.P95MissMs > 101 {
		t.Fatalf("unexpected miss p95: %v", r.GetHeader.P95MissMs)
	}
	if r.GetHeader.P95HitMs < 9 || r.GetHeader.P95HitMs > 11 {
		t.Fatalf("unexpected hit p95: %v", r.GetHeader.P95HitMs)
	}
	if r.GetHeader.P95ImprovementPct < 89 || r.GetHeader.P95ImprovementPct > 91 {
		t.Fatalf("unexpected improvement pct: %v", r.GetHeader.P95ImprovementPct)
	}

	// bid cache hit rate is 50%
	if r.GetHeader.CacheHitRatePct < 49 || r.GetHeader.CacheHitRatePct > 51 {
		t.Fatalf("unexpected hit rate: %v", r.GetHeader.CacheHitRatePct)
	}

	// no_bid rate: 1 out of 5 total getHeader calls = 20%
	if r.GetHeader.NoBidRatePct < 19 || r.GetHeader.NoBidRatePct > 21 {
		t.Fatalf("unexpected no_bid rate: %v", r.GetHeader.NoBidRatePct)
	}

	// lead time p95 should be ~500ms (bucketed)
	if r.Prefetch.LeadTimeP95Ms < 499 || r.Prefetch.LeadTimeP95Ms > 501 {
		t.Fatalf("unexpected lead time p95: %v", r.Prefetch.LeadTimeP95Ms)
	}

	if r.Value.WinningCount != 2 {
		t.Fatalf("unexpected winning count: %d", r.Value.WinningCount)
	}
	if r.Value.WinningSumETH < 0.029 || r.Value.WinningSumETH > 0.031 {
		t.Fatalf("unexpected winning sum eth: %v", r.Value.WinningSumETH)
	}
	if r.RelayWinners["relay-a"] != 2 {
		t.Fatalf("unexpected relay winners: %v", r.RelayWinners)
	}

	// Ensure state was reset.
	r2 := c.SnapshotAndReset(time.Unix(7200, 0))
	if r2.GetHeader.P95HitMs != 0 || r2.GetHeader.P95MissMs != 0 {
		t.Fatalf("expected empty stats after reset, got %+v", r2.GetHeader)
	}
}
