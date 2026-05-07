package obft

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
// realistic OBFT instance lifetime; short enough to bound memory under
// degenerate runner-crash scenarios.
const DefaultMaxAge = 32 * 12 * time.Second

// MaxDistinctPerOpSlot caps how many distinct envelope-hashes the limiter
// will admit for a single (kind, slot, op[, layer]) bucket. Beyond this cap,
// further distinct emissions are rejected at the rate-limiter — they couldn't
// add useful evidence at the protocol layer anyway (the operator is already
// flagged byzantine via Rule 2/3 on the second distinct emission).
//
// Without this cap, a byzantine streaming infinite distinct content from one
// op for one slot would accumulate entries unboundedly until TTL eviction.
// Tied to MaxCommitHashesPerOp on the protocol side (= 8) so neither layer
// is the weaker bound. Phase-1 bundles use the same cap per (op, layer); a
// byzantine leader emitting more than this many distinct V's per layer is
// already extreme.
const MaxDistinctPerOpSlot = 8

// RateLimiter dedups identical envelope bytes by (slot, operator, kind, hash).
// It exists at the message-validation boundary as gossipsub-flood mitigation:
// the same bytes redelivered by gossipsub are rejected before reaching
// validation/protocol layers, but DISTINCT content from the same operator is
// admitted so the protocol's equivocation-detection paths fire — Rule 3
// (cross-onion equivocation, second distinct KindCommit) and Rule 2 (leader
// equivocation, second distinct Phase-1 bundle).
//
// Memory management: each recorded entry carries a creation timestamp and
// is lazy-evicted by any Allow* call once older than MaxAge. Eviction is
// throttled to (MaxAge / 8) so sustained high-rate Allow* calls don't pay
// a full O(n) scan every time. Forget(slot) is an explicit O(n) cleanup
// hook the runner calls on instance completion. Per-(slot, op[, layer])
// bucketing via MaxDistinctPerOpSlot also bounds growth under sustained
// distinct-content abuse.
//
// At-rest behavior: stale entries clear on the next Allow* call after the
// throttle window MaxAge/8 has elapsed since the last eviction, or via
// explicit Forget(slot). Memory at rest is bounded by burst-size ×
// cluster-size — at SSV scale a few KiB in the worst case.
type RateLimiter struct {
	mu sync.Mutex

	// keyed by (slot, op, layer, hash) for bundles, (slot, op, hash) for
	// commit/cert. Hash makes "≤ 1 distinct content" the guarantee — same
	// content redelivered = drop, distinct content = admit (up to bucket cap).
	bundleSeen map[bundleKey]time.Time
	commitSeen map[opHashKey]time.Time
	certSeen   map[opHashKey]time.Time

	// Bucket counters: number of distinct hashes admitted per (slot, op[, layer])
	// for each kind. Used to enforce MaxDistinctPerOpSlot. Counter entries
	// are reaped lazily when their corresponding Seen entries TTL out (the
	// counter is recomputed on next Allow* via the seen-map) and explicitly
	// on Forget(slot). Buckets without a Seen entry have an implicit count
	// of zero; we only allocate counters when admitting.
	bundleCount map[bundleBucket]int
	commitCount map[opBucket]int
	certCount   map[opBucket]int

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
		bundleSeen:  make(map[bundleKey]time.Time),
		commitSeen:  make(map[opHashKey]time.Time),
		certSeen:    make(map[opHashKey]time.Time),
		bundleCount: make(map[bundleBucket]int),
		commitCount: make(map[opBucket]int),
		certCount:   make(map[opBucket]int),
		maxAge:      maxAge,
		now:         time.Now,
	}
}

// AllowPhase1Bundle dedups by (slot, op, layer, hash(body)). Returns an
// error only if the SAME body bytes were seen before, OR if the (slot, op,
// layer) bucket has hit MaxDistinctPerOpSlot. Distinct bytes within the cap
// (e.g., a byzantine leader's second equivocation bundle) are admitted so
// Rule 2 detection at the protocol layer fires.
func (r *RateLimiter) AllowPhase1Bundle(slot phase0.Slot, op spectypes.OperatorID, layer int, body []byte) error {
	hash := sha256.Sum256(body)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evictExpiredLocked()
	k := bundleKey{slot: slot, op: op, layer: layer, hash: hash}
	if _, exists := r.bundleSeen[k]; exists {
		return fmt.Errorf("obft adapter: rate limit: identical phase-1 bundle from operator %d at slot %d layer %d", op, slot, layer)
	}
	bucket := bundleBucket{slot: slot, op: op, layer: layer}
	if r.bundleCount[bucket] >= MaxDistinctPerOpSlot {
		return fmt.Errorf("obft adapter: rate limit: too many distinct phase-1 bundles from operator %d at slot %d layer %d (cap %d)",
			op, slot, layer, MaxDistinctPerOpSlot)
	}
	r.bundleSeen[k] = r.now()
	r.bundleCount[bucket]++
	return nil
}

// AllowCommit dedups by (slot, op, hash(body)). Returns an error only if
// the SAME body bytes were seen before, OR if the (slot, op) bucket has hit
// MaxDistinctPerOpSlot. Distinct bytes within the cap (e.g., byzantine's
// second distinct KindCommit) are admitted so Rule 3 detection at the
// protocol layer fires.
func (r *RateLimiter) AllowCommit(slot phase0.Slot, op spectypes.OperatorID, body []byte) error {
	hash := sha256.Sum256(body)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evictExpiredLocked()
	k := opHashKey{slot: slot, op: op, hash: hash}
	if _, exists := r.commitSeen[k]; exists {
		return fmt.Errorf("obft adapter: rate limit: identical KindCommit from operator %d at slot %d", op, slot)
	}
	bucket := opBucket{slot: slot, op: op}
	if r.commitCount[bucket] >= MaxDistinctPerOpSlot {
		return fmt.Errorf("obft adapter: rate limit: too many distinct KindCommits from operator %d at slot %d (cap %d)",
			op, slot, MaxDistinctPerOpSlot)
	}
	r.commitSeen[k] = r.now()
	r.commitCount[bucket]++
	return nil
}

// AllowCertificate dedups by (slot, op, hash(body)). Distinct content from
// the same operator is admitted up to MaxDistinctPerOpSlot (a byzantine
// emitting two valid certs would require forging the cluster pubkey, which
// is impossible within f-bound; any "second distinct cert" is malformed and
// gets rejected by VerifyCertificate at validation, but the cap still bounds
// memory under bytes-level abuse).
func (r *RateLimiter) AllowCertificate(slot phase0.Slot, op spectypes.OperatorID, body []byte) error {
	hash := sha256.Sum256(body)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evictExpiredLocked()
	k := opHashKey{slot: slot, op: op, hash: hash}
	if _, exists := r.certSeen[k]; exists {
		return fmt.Errorf("obft adapter: rate limit: identical KindCertificate from operator %d at slot %d", op, slot)
	}
	bucket := opBucket{slot: slot, op: op}
	if r.certCount[bucket] >= MaxDistinctPerOpSlot {
		return fmt.Errorf("obft adapter: rate limit: too many distinct KindCertificates from operator %d at slot %d (cap %d)",
			op, slot, MaxDistinctPerOpSlot)
	}
	r.certSeen[k] = r.now()
	r.certCount[bucket]++
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
	for k := range r.commitSeen {
		if k.slot == slot {
			delete(r.commitSeen, k)
		}
	}
	for k := range r.certSeen {
		if k.slot == slot {
			delete(r.certSeen, k)
		}
	}
	for b := range r.bundleCount {
		if b.slot == slot {
			delete(r.bundleCount, b)
		}
	}
	for b := range r.commitCount {
		if b.slot == slot {
			delete(r.commitCount, b)
		}
	}
	for b := range r.certCount {
		if b.slot == slot {
			delete(r.certCount, b)
		}
	}
}

// evictExpiredLocked removes entries older than maxAge. O(n) per call; n
// is bounded by ~maxAge worth of slots × cluster size. Throttled so we
// don't pay the full scan cost on every Allow* call under sustained
// message rate; lazy eviction with a (maxAge / 8) cooldown keeps
// memory bounded while amortizing the scan.
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
			if r.bundleCount[b] > 1 {
				r.bundleCount[b]--
			} else {
				delete(r.bundleCount, b)
			}
		}
	}
	for k, t := range r.commitSeen {
		if t.Before(cutoff) {
			delete(r.commitSeen, k)
			b := opBucket{slot: k.slot, op: k.op}
			if r.commitCount[b] > 1 {
				r.commitCount[b]--
			} else {
				delete(r.commitCount, b)
			}
		}
	}
	for k, t := range r.certSeen {
		if t.Before(cutoff) {
			delete(r.certSeen, k)
			b := opBucket{slot: k.slot, op: k.op}
			if r.certCount[b] > 1 {
				r.certCount[b]--
			} else {
				delete(r.certCount, b)
			}
		}
	}
}
