package consensustest

import (
	"math"
	"sort"
	"time"
)

// Distribution is a sample series with percentile / summary helpers. Stored
// internally as a sorted copy on demand — call sites should append samples
// and read summary methods after all samples are collected.
//
// Method semantics on an empty distribution: Mean / Min / Max / Stddev /
// Percentile / Median return 0; Len returns 0. This matches what callers
// expect when aggregating cells where the metric doesn't apply (e.g.
// DecisionTime for a 0%-success scenario — no samples means "no data",
// not "zero milliseconds").
type Distribution []float64

// Len is the sample count.
func (d Distribution) Len() int { return len(d) }

// Mean is the arithmetic mean.
func (d Distribution) Mean() float64 {
	if len(d) == 0 {
		return 0
	}
	var sum float64
	for _, v := range d {
		sum += v
	}
	return sum / float64(len(d))
}

// Stddev is the population standard deviation (divides by N, not N-1 —
// we treat the sample as the complete distribution for cell-aggregation
// purposes; sample-stddev with N-1 would be more correct for inferential
// statistics, but cell stats are descriptive).
func (d Distribution) Stddev() float64 {
	if len(d) == 0 {
		return 0
	}
	mean := d.Mean()
	var sum float64
	for _, v := range d {
		diff := v - mean
		sum += diff * diff
	}
	return math.Sqrt(sum / float64(len(d)))
}

// Min is the smallest sample (0 on empty).
func (d Distribution) Min() float64 {
	if len(d) == 0 {
		return 0
	}
	sorted := d.sorted()
	return sorted[0]
}

// Max is the largest sample (0 on empty).
func (d Distribution) Max() float64 {
	if len(d) == 0 {
		return 0
	}
	sorted := d.sorted()
	return sorted[len(sorted)-1]
}

// Median is the 50th percentile.
func (d Distribution) Median() float64 { return d.Percentile(50) }

// Percentile returns the p-th percentile (0-100) using linear interpolation
// between adjacent ranks. Boundaries: Percentile(0) == Min, Percentile(100)
// == Max. Returns 0 on an empty distribution.
//
// Interpolation rule: position = (p/100) * (N-1); if position is integer,
// return sorted[position]; else interpolate between sorted[floor(position)]
// and sorted[ceil(position)] by the fractional part.
func (d Distribution) Percentile(p float64) float64 {
	if len(d) == 0 {
		return 0
	}
	if p <= 0 {
		return d.Min()
	}
	if p >= 100 {
		return d.Max()
	}
	sorted := d.sorted()
	pos := (p / 100) * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return sorted[lo]
	}
	frac := pos - float64(lo)
	return sorted[lo] + frac*(sorted[hi]-sorted[lo])
}

// sorted returns a sorted copy of d. Internal helper; callers should hold
// onto the original slice.
func (d Distribution) sorted() []float64 {
	cp := make([]float64, len(d))
	copy(cp, d)
	sort.Float64s(cp)
	return cp
}

// BatchCell aggregates Iterations sims for one (protocol, scenario) pair.
//
// DecisionTime samples come ONLY from successful sims (deadlocks have no
// decision time). DecidingBroadcastTime is paired one-for-one with
// DecisionTime — same length, same per-sim index. SuccessRate gives the
// overall success fraction. ClusterBandwidth samples come from ALL sims
// regardless of decision — even a deadlock emits messages before it's
// declared a slot-miss, so bandwidth is observable. PerKindBandwidth
// breaks ClusterBandwidth down by MsgKind for stacked-chart rendering.
//
// EvidenceCounts is keyed by EvidenceRule (e.g. "OBFT/Rule4/FakeEncryptedPresence"
// or "QBFT/equivocation"); each Distribution holds per-sim total counts
// (one sample per sim, value = total fires across all operators in that sim).
//
// MissReasons aggregates per-sim deadlock causes — for now a simple
// string→count map keyed by a coarse classification (e.g. "no_quorum",
// "host_invalid", "asymmetric_propagation"). Free-form; renderer treats
// it as an attribute table.
//
// Safety violations are NOT carried on this struct — aggregateCellIters
// panics via SafetyPanic the moment ComputeSafetyReport flags a
// violation, so the batch run terminates immediately and the cell never
// gets emitted. Post-batch readers therefore see only safe data; there
// is no per-cell counter to consult.
type BatchCell struct {
	Protocol    string
	Scenario    string
	Iterations  int
	SuccessRate float64
	// DecisionTime carries one sample per successful sim. DecidingBroadcastTime
	// is the same length and is index-aligned with DecisionTime: entry i is
	// T_broadcast_max for the deciding layer of the i-th successful sim
	// (zero when the protocol has no slot-anchored broadcast, e.g. QBFT).
	// Reporting layer relies on the index alignment to emit parallel-sorted
	// arrays for the UI's slot_start adjustment.
	DecisionTime          Distribution
	DecidingBroadcastTime Distribution
	ClusterBandwidth      Distribution
	PerKindBandwidth      map[string]Distribution
	EvidenceCounts        map[string]Distribution
	MissReasons           map[string]int
}

// BatchReport is the top-level aggregate from a RunBatch call. Cells are
// stored flat (one per (protocol, scenario) pair); rendering layer groups
// them as needed.
type BatchReport struct {
	Config      BatchConfig
	Cells       []BatchCell
	Wallclock   time.Duration
	GeneratedAt time.Time
}
