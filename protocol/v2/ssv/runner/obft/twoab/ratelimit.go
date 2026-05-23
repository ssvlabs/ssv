package twoab

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// DefaultMaxAge is the per-entry TTL for rate-limiter records: 32 slots
// (~1 epoch on Ethereum mainnet at 12s slots = 6.4 min). Longer than any
// realistic 2abOBFT instance lifetime; short enough to bound memory under
// degenerate runner-crash scenarios.
//
// (Duplicated from the bare-OBFT runner per the separate-runner approach —
// the limiter buckets one additional pair of message kinds, KindValue /
// KindNoValue, so the type set differs.)
const DefaultMaxAge = 32 * 12 * time.Second

// MaxDistinctPerOpSlot caps how many distinct envelope-hashes the limiter
// will admit for a single (kind, slot, op[, layer]) bucket. Beyond this cap,
// further distinct emissions are rejected at the rate-limiter — they couldn't
// add useful evidence at the protocol layer anyway (the operator is already
// flagged byzantine via the relevant equivocation rule on the second distinct
// emission).
//
// Without this cap, a byzantine streaming infinite distinct content from one
// op for one slot would accumulate entries unboundedly until TTL eviction.
// Phase-1 bundles use the cap per (op, layer); the Phase-2a/2b kinds
// (KindValue / KindNoValue / KindCommit) and KindCertificate use it per
// (op, slot).
const MaxDistinctPerOpSlot = 8

// RateLimiter dedups identical envelope bytes by (slot, operator, kind, hash).
// It exists at the message-validation boundary as gossipsub-flood mitigation:
// the same bytes redelivered by gossipsub are rejected before reaching
// validation/protocol layers, but DISTINCT content from the same operator is
// admitted so the protocol's equivocation-detection paths fire — leader
// equivocation (second distinct Phase-1 bundle), and the 2abOBFT Phase-2
// equivocation rules (second distinct KindValue / KindNoValue / KindCommit).
//
// Memory management mirrors the bare-OBFT limiter: each recorded entry carries
// a creation timestamp and is lazy-evicted by any Allow* call once older than
// MaxAge. Eviction is throttled to (MaxAge / 8) so sustained high-rate Allow*
// calls don't pay a full O(n) scan every time. Forget(slot) is an explicit
// O(n) cleanup hook the runner calls on instance completion.
//
// 2abOBFT delta vs bare OBFT: five message kinds instead of three (KindValue
// and KindNoValue are the Phase-2a coordination envelopes, each keyed
// per (slot, op) like Commit / Certificate).
type RateLimiter struct {
	mu sync.Mutex

	// keyed by (slot, op, layer, hash) for bundles, (slot, op, hash) for the
	// per-op kinds. Hash makes "≤ 1 distinct content" the guarantee — same
	// content redelivered = drop, distinct content = admit (up to bucket cap).
	bundleSeen  map[bundleKey]time.Time
	valueSeen   map[opHashKey]time.Time
	noValueSeen map[opHashKey]time.Time
	commitSeen  map[opHashKey]time.Time
	certSeen    map[opHashKey]time.Time

	// Bucket counters: number of distinct hashes admitted per (slot, op[, layer])
	// for each kind. Used to enforce MaxDistinctPerOpSlot. Counter entries are
	// reaped lazily when their corresponding Seen entries TTL out and explicitly
	// on Forget(slot). Buckets without a Seen entry have an implicit count of
	// zero; we only allocate counters when admitting.
	bundleCount  map[bundleBucket]int
	valueCount   map[opBucket]int
	noValueCount map[opBucket]int
	commitCount  map[opBucket]int
	certCount    map[opBucket]int

	maxAge       time.Duration
	now          func() time.Time
	lastEviction time.Time // when evictExpiredLocked last ran
}

type opBucket struct {
	slot phase0.Slot
	op   spectypes.OperatorID
}

type bundleBucket struct {
	slot  phase0.Slot
	op    spectypes.OperatorID
	layer int
}

type opHashKey struct {
	slot phase0.Slot
	op   spectypes.OperatorID
	hash [32]byte
}

type bundleKey struct {
	slot  phase0.Slot
	op    spectypes.OperatorID
	layer int
	hash  [32]byte
}

// NewRateLimiter creates a fresh limiter with the default MaxAge.
func NewRateLimiter() *RateLimiter {
	return NewRateLimiterWithMaxAge(DefaultMaxAge)
}

// NewRateLimiterWithMaxAge creates a fresh limiter with a custom retention
// window. Use the default in production; override only for tests.
func NewRateLimiterWithMaxAge(maxAge time.Duration) *RateLimiter {
	return &RateLimiter{
		bundleSeen:   make(map[bundleKey]time.Time),
		valueSeen:    make(map[opHashKey]time.Time),
		noValueSeen:  make(map[opHashKey]time.Time),
		commitSeen:   make(map[opHashKey]time.Time),
		certSeen:     make(map[opHashKey]time.Time),
		bundleCount:  make(map[bundleBucket]int),
		valueCount:   make(map[opBucket]int),
		noValueCount: make(map[opBucket]int),
		commitCount:  make(map[opBucket]int),
		certCount:    make(map[opBucket]int),
		maxAge:       maxAge,
		now:          time.Now,
	}
}

// AllowPhase1Bundle dedups by (slot, op, layer, hash(body)). Returns an
// error only if the SAME body bytes were seen before, OR if the (slot, op,
// layer) bucket has hit MaxDistinctPerOpSlot. Distinct bytes within the cap
// (e.g., a byzantine leader's second equivocation bundle) are admitted so
// leader-equivocation detection at the protocol layer fires.
func (r *RateLimiter) AllowPhase1Bundle(slot phase0.Slot, op spectypes.OperatorID, layer int, body []byte) error {
	hash := sha256.Sum256(body)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evictExpiredLocked()
	k := bundleKey{slot: slot, op: op, layer: layer, hash: hash}
	if _, exists := r.bundleSeen[k]; exists {
		return fmt.Errorf("twoab adapter: rate limit: identical phase-1 bundle from operator %d at slot %d layer %d", op, slot, layer)
	}
	bucket := bundleBucket{slot: slot, op: op, layer: layer}
	if r.bundleCount[bucket] >= MaxDistinctPerOpSlot {
		return fmt.Errorf("twoab adapter: rate limit: too many distinct phase-1 bundles from operator %d at slot %d layer %d (cap %d)",
			op, slot, layer, MaxDistinctPerOpSlot)
	}
	r.bundleSeen[k] = r.now()
	r.bundleCount[bucket]++
	return nil
}

// AllowValueMsg dedups a Phase-2a KindValue by (slot, op, hash(body)).
// Distinct bytes within the cap (a byzantine's second distinct KindValue,
// cross-V equivocation) are admitted so the protocol's Rule-6a path fires.
func (r *RateLimiter) AllowValueMsg(slot phase0.Slot, op spectypes.OperatorID, body []byte) error {
	return r.allowOpKind(r.valueSeen, r.valueCount, slot, op, body, "KindValue")
}

// AllowNoValueMsg dedups a Phase-2a KindNoValue by (slot, op, hash(body)).
func (r *RateLimiter) AllowNoValueMsg(slot phase0.Slot, op spectypes.OperatorID, body []byte) error {
	return r.allowOpKind(r.noValueSeen, r.noValueCount, slot, op, body, "KindNoValue")
}

// AllowCommit dedups a KindCommit by (slot, op, hash(body)). Distinct bytes
// within the cap (a byzantine's second distinct KindCommit) are admitted so
// cross-side / cross-V equivocation detection at the protocol layer fires.
func (r *RateLimiter) AllowCommit(slot phase0.Slot, op spectypes.OperatorID, body []byte) error {
	return r.allowOpKind(r.commitSeen, r.commitCount, slot, op, body, "KindCommit")
}

// AllowCertificate dedups a KindCertificate by (slot, op, hash(body)).
// A second distinct cert would require forging the cluster pubkey (impossible
// within the f-bound) — any "second distinct cert" is malformed and rejected
// by VerifyCertificate at validation, but the cap still bounds memory under
// bytes-level abuse.
func (r *RateLimiter) AllowCertificate(slot phase0.Slot, op spectypes.OperatorID, body []byte) error {
	return r.allowOpKind(r.certSeen, r.certCount, slot, op, body, "KindCertificate")
}

// allowOpKind is the shared (slot, op, hash) admission path for the four
// per-op kinds (Value / NoValue / Commit / Certificate). The bundle kind
// keeps its own method because it buckets per (slot, op, layer).
func (r *RateLimiter) allowOpKind(
	seen map[opHashKey]time.Time,
	count map[opBucket]int,
	slot phase0.Slot,
	op spectypes.OperatorID,
	body []byte,
	label string,
) error {
	hash := sha256.Sum256(body)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evictExpiredLocked()
	k := opHashKey{slot: slot, op: op, hash: hash}
	if _, exists := seen[k]; exists {
		return fmt.Errorf("twoab adapter: rate limit: identical %s from operator %d at slot %d", label, op, slot)
	}
	bucket := opBucket{slot: slot, op: op}
	if count[bucket] >= MaxDistinctPerOpSlot {
		return fmt.Errorf("twoab adapter: rate limit: too many distinct %ss from operator %d at slot %d (cap %d)",
			label, op, slot, MaxDistinctPerOpSlot)
	}
	seen[k] = r.now()
	count[bucket]++
	return nil
}

// Forget releases per-slot tracking memory eagerly. Idempotent. With
// TTL-based eviction this is no longer required for correctness, but the
// runner calls it on instance completion to free memory promptly.
func (r *RateLimiter) Forget(slot phase0.Slot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k := range r.bundleSeen {
		if k.slot == slot {
			delete(r.bundleSeen, k)
		}
	}
	for _, seen := range []map[opHashKey]time.Time{r.valueSeen, r.noValueSeen, r.commitSeen, r.certSeen} {
		for k := range seen {
			if k.slot == slot {
				delete(seen, k)
			}
		}
	}
	for b := range r.bundleCount {
		if b.slot == slot {
			delete(r.bundleCount, b)
		}
	}
	for _, count := range []map[opBucket]int{r.valueCount, r.noValueCount, r.commitCount, r.certCount} {
		for b := range count {
			if b.slot == slot {
				delete(count, b)
			}
		}
	}
}

// evictExpiredLocked removes entries older than maxAge. O(n) per call; n is
// bounded by ~maxAge worth of slots × cluster size. Throttled so we don't pay
// the full scan cost on every Allow* call under sustained message rate; lazy
// eviction with a (maxAge / 8) cooldown keeps memory bounded while amortizing
// the scan.
//
// Counter buckets are decremented in lockstep with each Seen entry that's
// reaped, so counts stay consistent with the seen-maps. A bucket whose count
// hits zero is removed entirely.
func (r *RateLimiter) evictExpiredLocked() {
	now := r.now()
	if !r.lastEviction.IsZero() && now.Sub(r.lastEviction) < r.maxAge/8 {
		return
	}
	r.lastEviction = now
	cutoff := now.Add(-r.maxAge)
	for k, t := range r.bundleSeen {
		if t.Before(cutoff) {
			delete(r.bundleSeen, k)
			b := bundleBucket{slot: k.slot, op: k.op, layer: k.layer}
			decBucket(r.bundleCount, b)
		}
	}
	for _, pair := range []struct {
		seen  map[opHashKey]time.Time
		count map[opBucket]int
	}{
		{r.valueSeen, r.valueCount},
		{r.noValueSeen, r.noValueCount},
		{r.commitSeen, r.commitCount},
		{r.certSeen, r.certCount},
	} {
		for k, t := range pair.seen {
			if t.Before(cutoff) {
				delete(pair.seen, k)
				decBucket(pair.count, opBucket{slot: k.slot, op: k.op})
			}
		}
	}
}

// decBucket decrements a counter bucket in lockstep with a reaped Seen entry,
// removing the bucket entirely once it hits zero.
func decBucket[K comparable](count map[K]int, b K) {
	if count[b] > 1 {
		count[b]--
	} else {
		delete(count, b)
	}
}
