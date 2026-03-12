package stats

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

type Collector struct {
	mu sync.Mutex

	windowStart time.Time

	durationBounds []float64
	slotBounds     []float64
	leadBounds     []float64
	bidValueBounds []float64

	getHeader           map[string]*Histogram
	getHeaderSlot       map[string]*Histogram
	getHeaderTotal      map[string]uint64 // key=cache|result
	getHeaderBidTotal   map[string]uint64 // key=cache
	prefetchRequests    uint64
	prefetchSkips       map[string]uint64 // skip_reason
	prefetchResults     map[string]uint64 // result
	prefetchLeadTime    *Histogram
	prefetchFirstCached *Histogram
	prefetchLate        uint64
	prefetchOverSlot    uint64
	prefetchParentHash  map[string]uint64 // compare_result
	bidValueETH         *Histogram
	bidValueETHSum      float64
	bidValueETHCount    uint64
	relayWinnerCounts   map[string]uint64            // relay host
	relayWinnerBySource map[string]map[string]uint64 // source -> relay host -> count
}

type Options struct {
	DurationBoundsSeconds []float64
	SlotBoundsSeconds     []float64
	LeadTimeBoundsSeconds []float64
	BidValueETHBounds     []float64
}

func NewCollector(opts Options) *Collector {
	now := time.Now()

	durationBounds := defaultSecondsBounds(opts.DurationBoundsSeconds)
	slotBounds := defaultSlotBounds(opts.SlotBoundsSeconds)
	leadBounds := defaultSlotBounds(opts.LeadTimeBoundsSeconds)
	bidBounds := defaultBidValueBounds(opts.BidValueETHBounds)

	return &Collector{
		windowStart: now,

		durationBounds: durationBounds,
		slotBounds:     slotBounds,
		leadBounds:     leadBounds,
		bidValueBounds: bidBounds,

		getHeader:         make(map[string]*Histogram),
		getHeaderSlot:     make(map[string]*Histogram),
		getHeaderTotal:    make(map[string]uint64),
		getHeaderBidTotal: make(map[string]uint64),

		prefetchSkips:   make(map[string]uint64),
		prefetchResults: make(map[string]uint64),

		prefetchLeadTime:    NewHistogram(leadBounds),
		prefetchFirstCached: NewHistogram(leadBounds),
		prefetchParentHash:  make(map[string]uint64),

		bidValueETH:         NewHistogram(bidBounds),
		relayWinnerCounts:   make(map[string]uint64),
		relayWinnerBySource: make(map[string]map[string]uint64),
	}
}

func defaultSecondsBounds(bounds []float64) []float64 {
	if len(bounds) == 0 {
		return []float64{0, 0.001, 0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}
	}
	return bounds
}

func defaultSlotBounds(bounds []float64) []float64 {
	if len(bounds) == 0 {
		// Slot-relative numbers are meaningful up to ~12s.
		return []float64{0, 0.05, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10, 12}
	}
	return bounds
}

func defaultBidValueBounds(bounds []float64) []float64 {
	if len(bounds) == 0 {
		// Values are typically small (sub-ETH) but can spike; keep buckets coarse.
		return []float64{0, 1e-6, 1e-5, 1e-4, 1e-3, 5e-3, 1e-2, 5e-2, 0.1, 0.25, 0.5, 1, 2, 5, 10, 25, 50}
	}
	return bounds
}

func keyGetHeader(cache, result string) string {
	return cache + "|" + result
}

func (c *Collector) ObserveGetHeader(cache, result string, duration time.Duration, slotOffset time.Duration) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.windowStart.IsZero() {
		c.windowStart = time.Now()
	}

	k := keyGetHeader(cache, result)
	h := c.getHeader[k]
	if h == nil {
		h = NewHistogram(c.durationBounds)
		c.getHeader[k] = h
	}
	h.Record(duration.Seconds())

	hs := c.getHeaderSlot[k]
	if hs == nil {
		hs = NewHistogram(c.slotBounds)
		c.getHeaderSlot[k] = hs
	}
	off := slotOffset
	if off < 0 {
		off = 0
	}
	if off > 12*time.Second {
		off = 12 * time.Second
	}
	hs.Record(off.Seconds())

	c.getHeaderTotal[k]++
	if result == "bid" {
		c.getHeaderBidTotal[cache]++
	}
}

func (c *Collector) ObservePrefetchLeadTime(lead time.Duration) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if lead < 0 {
		c.prefetchLate++
		lead = 0
	}
	if lead > 12*time.Second {
		c.prefetchOverSlot++
		lead = 12 * time.Second
	}
	c.prefetchLeadTime.Record(lead.Seconds())
}

func (c *Collector) ObservePrefetchFirstCachedLeadTime(lead time.Duration) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if lead < 0 {
		lead = 0
	}
	if lead > 12*time.Second {
		lead = 12 * time.Second
	}
	c.prefetchFirstCached.Record(lead.Seconds())
}

func (c *Collector) ObservePrefetchRequest() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prefetchRequests++
}

func (c *Collector) ObservePrefetchSkip(reason string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prefetchSkips[reason]++
}

func (c *Collector) ObservePrefetchResult(result string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prefetchResults[result]++
}

func (c *Collector) ObservePrefetchParentHashCompare(result string) {
	if c == nil || result == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prefetchParentHash[result]++
}

func (c *Collector) ObserveWinningBid(source, relayHost string, valueETH float64) {
	if c == nil {
		return
	}
	if relayHost == "" || math.IsNaN(valueETH) || math.IsInf(valueETH, 0) {
		return
	}
	if valueETH < 0 {
		valueETH = 0
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.bidValueETH.Record(valueETH)
	c.bidValueETHSum += valueETH
	c.bidValueETHCount++

	c.relayWinnerCounts[relayHost]++
	if c.relayWinnerBySource[source] == nil {
		c.relayWinnerBySource[source] = make(map[string]uint64)
	}
	c.relayWinnerBySource[source][relayHost]++
}

// PrefetchObserver hooks (matches bidcache callbacks).
func (c *Collector) OnPrefetchRequest(_ context.Context)             { c.ObservePrefetchRequest() }
func (c *Collector) OnPrefetchSkip(_ context.Context, reason string) { c.ObservePrefetchSkip(reason) }
func (c *Collector) OnPrefetchResult(_ context.Context, result string) {
	c.ObservePrefetchResult(result)
}

type Report struct {
	WindowSeconds float64 `json:"window_seconds"`

	GetHeader struct {
		P95HitMs          float64 `json:"p95_hit_ms,omitempty"`
		P95MissMs         float64 `json:"p95_miss_ms,omitempty"`
		P95ImprovementPct float64 `json:"p95_improvement_pct,omitempty"`
		CacheHitRatePct   float64 `json:"cache_hit_rate_pct,omitempty"`
		NoBidRatePct      float64 `json:"no_bid_rate_pct,omitempty"`
		ErrorRatePct      float64 `json:"error_rate_pct,omitempty"`
	} `json:"get_header"`

	SlotOffset struct {
		P95HitMs          float64 `json:"p95_hit_ms,omitempty"`
		P95MissMs         float64 `json:"p95_miss_ms,omitempty"`
		P95ImprovementPct float64 `json:"p95_improvement_pct,omitempty"`
	} `json:"slot_offset"`

	Prefetch struct {
		Requests                  uint64            `json:"requests,omitempty"`
		LateTotal                 uint64            `json:"late_total,omitempty"`
		OverSlotTotal             uint64            `json:"over_slot_total,omitempty"`
		LeadTimeP95Ms             float64           `json:"lead_time_p95_ms,omitempty"`
		FirstCachedLeadTimeP95Ms  float64           `json:"first_cached_lead_time_p95_ms,omitempty"`
		ParentHashCompare         map[string]uint64 `json:"parent_hash_compare,omitempty"`
		ParentHashMismatchRatePct float64           `json:"parent_hash_mismatch_rate_pct,omitempty"`
		Results                   map[string]uint64 `json:"results,omitempty"`
		Skips                     map[string]uint64 `json:"skips,omitempty"`
	} `json:"prefetch"`

	Value struct {
		WinningCount  uint64  `json:"winning_count,omitempty"`
		WinningSumETH float64 `json:"winning_sum_eth,omitempty"`
		WinningAvgETH float64 `json:"winning_avg_eth,omitempty"`
		P95ETH        float64 `json:"p95_eth,omitempty"`
	} `json:"value"`

	RelayWinners         map[string]uint64            `json:"relay_winners,omitempty"`
	RelayWinnersBySource map[string]map[string]uint64 `json:"relay_winners_by_source,omitempty"`
}

func (c *Collector) SnapshotAndReset(now time.Time) Report {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.windowStart.IsZero() {
		c.windowStart = now
	}
	window := now.Sub(c.windowStart)
	if window <= 0 {
		window = time.Second
	}

	report := Report{
		WindowSeconds:        window.Seconds(),
		RelayWinners:         make(map[string]uint64),
		RelayWinnersBySource: make(map[string]map[string]uint64),
	}

	// Helper to compute p95 for specific keys.
	p95Ms := func(h *Histogram) float64 {
		if h == nil {
			return 0
		}
		v, ok := h.QuantileUpperBound(0.95)
		if !ok {
			return 0
		}
		return v * 1000
	}

	// getHeader: compare cache hit vs miss for bids.
	hitKey := keyGetHeader("hit", "bid")
	missKey := keyGetHeader("miss", "bid")
	report.GetHeader.P95HitMs = p95Ms(c.getHeader[hitKey])
	report.GetHeader.P95MissMs = p95Ms(c.getHeader[missKey])
	if report.GetHeader.P95MissMs > 0 && report.GetHeader.P95HitMs > 0 {
		report.GetHeader.P95ImprovementPct = (report.GetHeader.P95MissMs - report.GetHeader.P95HitMs) / report.GetHeader.P95MissMs * 100
	}

	// cache hit rate for bids.
	bidHit := c.getHeaderBidTotal["hit"]
	bidMiss := c.getHeaderBidTotal["miss"]
	bidTotal := bidHit + bidMiss
	if bidTotal > 0 {
		report.GetHeader.CacheHitRatePct = float64(bidHit) / float64(bidTotal) * 100
	}

	// no_bid and error rates across all get_header requests.
	var totalRequests uint64
	var noBid uint64
	var errs uint64
	for k, n := range c.getHeaderTotal {
		totalRequests += n
		_, result, ok := splitKey(k)
		if !ok {
			continue
		}
		switch result {
		case "no_bid":
			noBid += n
		case "error":
			errs += n
		}
	}
	if totalRequests > 0 {
		report.GetHeader.NoBidRatePct = float64(noBid) / float64(totalRequests) * 100
		report.GetHeader.ErrorRatePct = float64(errs) / float64(totalRequests) * 100
	}

	// Slot offset p95 hit vs miss for bids.
	report.SlotOffset.P95HitMs = p95Ms(c.getHeaderSlot[hitKey])
	report.SlotOffset.P95MissMs = p95Ms(c.getHeaderSlot[missKey])
	if report.SlotOffset.P95MissMs > 0 && report.SlotOffset.P95HitMs > 0 {
		report.SlotOffset.P95ImprovementPct = (report.SlotOffset.P95MissMs - report.SlotOffset.P95HitMs) / report.SlotOffset.P95MissMs * 100
	}

	// Prefetch
	report.Prefetch.Requests = c.prefetchRequests
	report.Prefetch.LateTotal = c.prefetchLate
	report.Prefetch.OverSlotTotal = c.prefetchOverSlot
	report.Prefetch.Results = copyUint64Map(c.prefetchResults)
	report.Prefetch.Skips = copyUint64Map(c.prefetchSkips)
	report.Prefetch.LeadTimeP95Ms = p95Ms(c.prefetchLeadTime)
	report.Prefetch.FirstCachedLeadTimeP95Ms = p95Ms(c.prefetchFirstCached)
	report.Prefetch.ParentHashCompare = copyUint64Map(c.prefetchParentHash)
	matches := c.prefetchParentHash["match"]
	mismatches := c.prefetchParentHash["mismatch"]
	known := matches + mismatches
	if known > 0 {
		report.Prefetch.ParentHashMismatchRatePct = float64(mismatches) / float64(known) * 100
	}

	// Value
	report.Value.WinningCount = c.bidValueETHCount
	report.Value.WinningSumETH = c.bidValueETHSum
	if c.bidValueETHCount > 0 {
		report.Value.WinningAvgETH = c.bidValueETHSum / float64(c.bidValueETHCount)
	}
	if v, ok := c.bidValueETH.QuantileUpperBound(0.95); ok {
		report.Value.P95ETH = v
	}

	report.RelayWinners = copyUint64Map(c.relayWinnerCounts)
	for source, m := range c.relayWinnerBySource {
		report.RelayWinnersBySource[source] = copyUint64Map(m)
	}

	// Reset internal state for next window.
	c.windowStart = now
	for _, h := range c.getHeader {
		h.Reset()
	}
	for _, h := range c.getHeaderSlot {
		h.Reset()
	}
	clear(c.getHeader)
	clear(c.getHeaderSlot)
	clear(c.getHeaderTotal)
	clear(c.getHeaderBidTotal)
	c.prefetchRequests = 0
	clear(c.prefetchSkips)
	clear(c.prefetchResults)
	c.prefetchLeadTime.Reset()
	c.prefetchFirstCached.Reset()
	c.prefetchLate = 0
	c.prefetchOverSlot = 0
	clear(c.prefetchParentHash)
	c.bidValueETH.Reset()
	c.bidValueETHSum = 0
	c.bidValueETHCount = 0
	clear(c.relayWinnerCounts)
	clear(c.relayWinnerBySource)

	return report
}

func splitKey(k string) (cache string, result string, ok bool) {
	cache, result, ok = strings.Cut(k, "|")
	if !ok || cache == "" || result == "" {
		return "", "", false
	}
	return cache, result, true
}

func copyUint64Map(in map[string]uint64) map[string]uint64 {
	out := make(map[string]uint64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (r Report) TopRelays(n int) []string {
	type kv struct {
		k string
		v uint64
	}
	arr := make([]kv, 0, len(r.RelayWinners))
	for k, v := range r.RelayWinners {
		arr = append(arr, kv{k: k, v: v})
	}
	sort.Slice(arr, func(i, j int) bool {
		if arr[i].v == arr[j].v {
			return arr[i].k < arr[j].k
		}
		return arr[i].v > arr[j].v
	})
	out := make([]string, 0, n)
	for i := 0; i < len(arr) && i < n; i++ {
		out = append(out, arr[i].k)
	}
	return out
}
