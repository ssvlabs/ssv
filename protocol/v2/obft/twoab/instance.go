package twoab

import (
	"errors"
	"fmt"
	"time"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
)

// Instance is the per-slot 2abOBFT consensus state machine.
//
// Lifecycle (per docs/2abOBFT.md §Slot structure):
//
//  1. Phase 1 — [slot_start, T_verdict_start]:
//     a. If local operator is a layer leader: BuildPhase1Bundle(layer, V)
//     to construct a bundle and broadcast it. NO σ_V partial (Variant C).
//     b. ObservePhase1Bundle(b, observedOffset) for every retained peer
//     bundle, with auth-only retention for bundles past T_accept_max.
//
//  2. Phase 2a — [T_verdict_start, T_commit]:
//     a. BuildVerdict(layer) at T_verdict_max − ε_proc per layer.
//     (Phase F — not yet implemented.)
//     b. ObserveVerdict(v) for peer verdicts. (Phase F.)
//
//  3. Phase 2b — at T_commit:
//     a. BuildOwnOnion2b() — emit a single KindOnion2b message carrying
//     σ-or-NR partials per layer based on the convergence-rule
//     decision over the observed Phase-2a verdict pool.
//     (Phase G.)
//     b. ObserveOnion2b(o) for peers. (Phase G.)
//
//  4. Phase 3 — from T_commit + Delta2b onward:
//     a. Resolve() → Output (success) or ErrNoQuorum. (Phase H.)
//     b. On success: BuildCertificate(out) → broadcast. (Phase H.)
//     c. ObserveCertificate(c) — fallback submission path. (Phase H.)
//
// Instance is NOT thread-safe; callers must serialize access. The
// expected SSV adapter runs each Instance behind its own mutex (Phase L).
type Instance struct {
	cfg           *Config
	ownOperatorID OperatorID

	// Cryptographic dependencies — declared at construction time.
	// Bundle observation (Phase E) doesn't need signer or ibe (bundles
	// have no threshold partials in 2ab Variant C); these fields are
	// populated for the later phases (G/H) that consume them.
	signer          obft.Signer
	tagSigner       obft.Signer
	ibe             obft.ThresholdIBE
	clusterPubKey   []byte
	pubKeyShares    map[OperatorID][]byte
	ibePubKeyShares map[OperatorID][]byte // optional under Option A

	// retainedBundles[layer][leader_id] = bundles retained for this
	// (layer, leader). Capped at 2 distinct value_roots per spec §Phase 1
	// / Retention bounds. Each entry carries an AuthOnly flag indicating
	// whether the bundle was first-observed past T_accept_max (auth-only
	// retention; not verdict-eligible) or in-window (regular retention).
	//
	// Phase F's verdict computation reads this state; auth-only entries
	// don't drive the operator's own verdict but DO let the receiver
	// verify peer-re-flooded V claims at the wire layer.
	retainedBundles map[int]map[OperatorID][]*retainedBundle

	// hostVerdict[layer][string(value_root)] = host application's
	// valid/not-valid verdict on the V identified by value_root at this
	// layer. Populated via ApplyHostValidity; consumed by
	// ComputeLocalVerdict at Phase-2a verdict-broadcast time. Per spec
	// §Phase 2a, the host is re-consulted at verdict-broadcast time
	// (potentially with a more recent head snapshot than at Phase-1
	// acceptance — narrows the divergence window structurally).
	hostVerdict map[int]map[string]bool

	// ownVerdict[layer] = the local operator's broadcast Verdict at this
	// layer, cached after BuildVerdict. Subsequent BuildVerdict calls at
	// the same layer return the cached entry (idempotent). Per spec
	// §Phase 2a, each operator emits exactly one Verdict per (slot,
	// layer); a second distinct Verdict from this operator would be
	// Rule-6a self-equivocation.
	ownVerdict map[int]*Verdict

	// peerVerdicts[layer][op] = the first-observed Verdict from op at
	// this layer. Per spec §Phase 2a, honest receivers count the
	// first-observed verdict for convergence purposes; subsequent
	// distinct verdicts are dropped from convergence input but recorded
	// as Rule-6a evidence.
	peerVerdicts map[int]map[OperatorID]*Verdict

	// verdictEquivocator[layer][op] = true if op has been observed
	// broadcasting two distinct verdicts at this layer. Per spec
	// §Phase 2a "Treat-equivocator-as-null (SHOULD)": receivers SHOULD
	// treat operator i's contribution as null in the convergence count
	// once a second distinct KindVerdict from i for the same (slot,
	// layer) has been observed before convergence-rule evaluation at
	// Phase-2b start. Phase G's convergence rule reads this map and
	// excludes flagged operators from both verdict_pool and nr_pool.
	verdictEquivocator map[int]map[OperatorID]bool

	// Evidence accumulation. Per spec §Slashing evidence, the observer
	// (if set) fires on FIRST recording per (Rule, OperatorID, Layer)
	// tuple; subsequent records of the same logical fault are kept in
	// the slice for full audit but don't re-fire the observer.
	evidence         []Evidence
	evidenceObserver EvidenceObserver
	evidenceObserved map[evidenceObservedKey]bool

	// Phase E doesn't yet need per-rule dedup buckets beyond
	// evidenceObserved; Rules 1 / 3 / 4 / 5 / 6b dedup will accrete
	// as Phases G/I land.

	ended bool
}

// retainedBundle wraps a Phase-1 bundle with the metadata Phase E needs
// to track retention semantics.
type retainedBundle struct {
	// Bundle is a deep copy of the retained bundle (defensive against
	// caller-owned slice mutation post-Observe).
	Bundle *Phase1Bundle

	// AuthOnly is true if the bundle was first-observed past T_accept_max
	// (= T_commit − 1 BTT). Auth-only-retained bundles satisfy spec
	// §Phase 1 / "auth-only retention": the leader's auth signature is
	// retained for the slot (usable for verifying peer-re-flooded V's
	// during Phase-2a), but the bundle does NOT drive the receiving
	// operator's own Phase-2a verdict.
	AuthOnly bool

	// FirstObservedAt is the offset (from slot_start) at which the
	// receiver first observed this bundle. Useful for tests + diagnostics.
	FirstObservedAt time.Duration
}

// NewInstance constructs a 2abOBFT Instance. Validates the config and
// pubKeyShares at construction; the constructor signature mirrors
// base.NewInstance for cross-protocol consistency.
//
// `signer` is the operator's V-keypair share signer. `tagSigner` is the
// operator's IBE-keypair share signer used for NR partials (may equal
// `signer` under Option A). `ibe` is the threshold-IBE primitive.
//
// `clusterPubKey` is the cluster's V-keypair aggregate pubkey, used to
// verify final Certificate signatures.
//
// `pubKeyShares` maps each operator's ID to their V-keypair pubkey
// share. `ibePubKeyShares` is the same for the IBE keypair; may be nil
// under Option A.
//
// `evidenceObserver`, if non-nil, fires once per (Rule, OperatorID,
// Layer) tuple on first observation. Set at construction so it's
// immutable post-construction.
func NewInstance(
	cfg *Config,
	ownOperatorID OperatorID,
	signer obft.Signer,
	tagSigner obft.Signer,
	ibe obft.ThresholdIBE,
	clusterPubKey []byte,
	pubKeyShares map[OperatorID][]byte,
	ibePubKeyShares map[OperatorID][]byte,
	evidenceObserver EvidenceObserver,
) (*Instance, error) {
	if cfg == nil {
		return nil, ErrNilConfig
	}
	if signer == nil || ibe == nil {
		return nil, errors.New("twoab: nil signer or ibe")
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("twoab: invalid config: %w", err)
	}
	if pubKeyShares == nil {
		return nil, errors.New("twoab: nil pubKeyShares (need at least an empty map)")
	}
	if !operatorInCluster(ownOperatorID, cfg) {
		return nil, fmt.Errorf("twoab: own operator id %d not in cluster", ownOperatorID)
	}
	// Every cluster member must have a registered pub-key share — leaders
	// need it for Phase-3 reconstruction; non-leader peers need it so
	// Phase-2b σ-verification paths work against their share. Missing
	// shares are surfaced upfront so the misconfiguration is caught in
	// test/setup rather than degrading evidence detection later.
	for _, op := range cfg.Operators {
		if share, ok := pubKeyShares[op]; !ok || len(share) == 0 {
			return nil, fmt.Errorf("twoab: operator %d has no pub-key share", op)
		}
	}
	if tagSigner == nil {
		tagSigner = signer
	}

	K := cfg.K()
	return &Instance{
		cfg:                cfg,
		ownOperatorID:      ownOperatorID,
		signer:             signer,
		tagSigner:          tagSigner,
		ibe:                ibe,
		clusterPubKey:      clusterPubKey,
		pubKeyShares:       pubKeyShares,
		ibePubKeyShares:    ibePubKeyShares,
		evidenceObserver:   evidenceObserver,
		retainedBundles:    make(map[int]map[OperatorID][]*retainedBundle, K),
		hostVerdict:        make(map[int]map[string]bool, K),
		ownVerdict:         make(map[int]*Verdict, K),
		peerVerdicts:       make(map[int]map[OperatorID]*Verdict, K),
		verdictEquivocator: make(map[int]map[OperatorID]bool, K),
		evidenceObserved:   make(map[evidenceObservedKey]bool),
	}, nil
}

// Config returns the instance's config (read-only).
func (i *Instance) Config() *Config { return i.cfg }

// OwnOperatorID returns the local operator's ID.
func (i *Instance) OwnOperatorID() OperatorID { return i.ownOperatorID }

// LeaderAtLayers returns the layer indices where the local operator is
// the designated leader. Empty if the operator is not a leader at any
// layer in this slot.
func (i *Instance) LeaderAtLayers() []int {
	var out []int
	for k, l := range i.cfg.Layers {
		if l.Leader == i.ownOperatorID {
			out = append(out, k)
		}
	}
	return out
}

// Evidence returns a snapshot copy of accumulated evidence entries.
// Order is insertion order. Safe to call concurrently with the Instance
// only if the caller serializes access.
func (i *Instance) Evidence() []Evidence {
	out := make([]Evidence, len(i.evidence))
	copy(out, i.evidence)
	return out
}

// RetainedBundles returns the bundles retained at (layer, leader_id) at
// the moment of the call. Includes both regular and auth-only retentions.
// Returns nil if no bundles are retained for this key.
//
// Returned slice is a snapshot copy — caller can iterate without
// affecting Instance state. The pointed-to *Phase1Bundle structs are
// shared (read-only contract); callers MUST NOT mutate them.
func (i *Instance) RetainedBundles(layer int, leaderID OperatorID) []*retainedBundle {
	leaderMap := i.retainedBundles[layer]
	if leaderMap == nil {
		return nil
	}
	src := leaderMap[leaderID]
	if len(src) == 0 {
		return nil
	}
	out := make([]*retainedBundle, len(src))
	copy(out, src)
	return out
}

// Ended reports whether the instance has been Finalized.
func (i *Instance) Ended() bool { return i.ended }

// Finalize closes the Instance. Phase-E stub — Phases F-H add cleanup
// hooks. Idempotent.
func (i *Instance) Finalize() {
	i.ended = true
}

// recordEvidence appends a non-nil evidence entry to the accumulator and
// fires the EvidenceObserver on first observation per (Rule, OperatorID,
// Layer) tuple.
func (i *Instance) recordEvidence(e Evidence) {
	i.evidence = append(i.evidence, e)
	if i.evidenceObserver == nil {
		return
	}
	key := evidenceObservedKey{rule: e.Rule, op: e.OperatorID, layer: e.Layer}
	if i.evidenceObserved[key] {
		return
	}
	i.evidenceObserved[key] = true
	i.evidenceObserver(e)
}
