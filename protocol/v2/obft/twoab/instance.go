package twoab

import (
	"bytes"
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

	// Phase-2b local commitment state — per-layer σ/NR lock + cached σ
	// partials. EKM enforces single-σ-V and σ-XOR-NR per (slot, layer)
	// at sign time.
	sigmaLocked  []bool
	sigmaLockedV []Value // when sigmaLocked[k], sigmaLockedV[k] is the V signed
	nrLocked     []bool
	ownPartials  map[int]Signature // cached σ partials per layer

	// ownOnion2b is the cached local Phase-2b emission. Set once by
	// BuildOwnOnion2b; subsequent calls return the same instance per
	// spec §Phase 2b (single emission per slot per operator).
	ownOnion2b *Onion2b

	// peerOnions[layer][op] = the σ-side onion entries seen from this
	// operator at this layer (one per Onion2b they emitted). Multiple
	// distinct entries means cross-onion equivocation (Rule 3).
	peerOnions map[int]map[OperatorID][]EncryptedLayer

	// peerNR[layer][op] = the operator's NR partial for this layer (one
	// per Onion2b they emitted).
	peerNR map[int]map[OperatorID]Signature

	// peerOnion2bHashes[op] = set of content hashes observed from op.
	// Identical re-broadcasts are no-ops; the first distinct second hash
	// is top-level Rule-3 cross-onion equivocation.
	peerOnion2bHashes map[OperatorID]map[[32]byte]struct{}

	// peerFirstOnion2b retains a deep copy of the FIRST Onion2b observed
	// from each operator so the second distinct emission can package
	// both bodies into Rule-3 evidence at Layer=-1.
	peerFirstOnion2b map[OperatorID]*Onion2b

	// Per-rule dedup buckets — ensure one Evidence entry per logical
	// fault even when multiple detection paths fire.
	rule1Fired       map[int]map[OperatorID]bool // Rule 1 cross-signing per (layer, op)
	rule3LeaderFired map[int]map[OperatorID]bool // Rule 3 per (layer, op) at L_0
	rule4Fired       map[int]map[OperatorID]bool // Rule 4 fake encrypted-presence per (layer, op)
	rule5Fired       map[int]map[OperatorID]bool // Rule 5 (cryptoFake or unknownV) per (layer, op)
	rule6bFired      map[int]map[OperatorID]bool // Rule 6b per (layer, op)

	// receivedCertificate is the FIRST peer-broadcast Certificate
	// observed via ObserveCertificate at this Instance. Per spec
	// §Final-certificate gossip, surviving peers' certificates allow an
	// operator that failed to reconstruct locally to submit (V, S)
	// downstream — protects against the lone-reconstructor's beacon-
	// path-fails failure mode. Subsequent ObserveCertificate calls
	// (post-first) are silent dedup no-ops.
	receivedCertificate *Certificate

	// Evidence accumulation. Per spec §Slashing evidence, the observer
	// (if set) fires on FIRST recording per (Rule, OperatorID, Layer)
	// tuple; subsequent records of the same logical fault are kept in
	// the slice for full audit but don't re-fire the observer.
	evidence         []Evidence
	evidenceObserver EvidenceObserver
	evidenceObserved map[evidenceObservedKey]bool

	// l0VerdictReadyCh is closed when the operator can determine their L_0
	// Phase-2a verdict — uniquely-retained host-validated V, OR host
	// not-valid on the unique retained V, OR ≥ 2 retained V's (equivocation
	// observed forcing NR), OR for an L_0 leader the moment they've built
	// their own L_0 bundle. Closed at most once per slot. Phase-2a verdict
	// emit selects on this against the T_verdict_max-ε_proc fallback.
	l0VerdictReadyCh chan struct{}

	// l0SigmaEligibilityCh is closed when the operator's local verdict-pool
	// at L_0 has ≥ qV σV verdicts for some V (i.e., the convergence rule
	// will yield a stable σ output at L_0). Closed at most once per slot.
	// Phase-2b commit emit selects on this against the T_commit fallback.
	l0SigmaEligibilityCh chan struct{}

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

	// RetentionEstablishedAt is the offset (from slot_start) at which
	// this retention entry took its current (AuthOnly) status:
	//
	//   - First observation of a previously-unseen V at any retention
	//     mode → set to that observation's offset.
	//   - Auth-only → regular promotion (a previously auth-only-retained
	//     V is re-observed within the accept window) → updated to the
	//     promotion observation's offset.
	//   - Identical re-broadcast within the same retention mode (silent
	//     dedup) → NOT updated.
	//
	// Useful for tests + diagnostics.
	RetentionEstablishedAt time.Duration
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
		sigmaLocked:        make([]bool, K),
		sigmaLockedV:       make([]Value, K),
		nrLocked:           make([]bool, K),
		ownPartials:        make(map[int]Signature, K),
		peerOnions:         make(map[int]map[OperatorID][]EncryptedLayer, K),
		peerNR:             make(map[int]map[OperatorID]Signature, K),
		peerOnion2bHashes:  make(map[OperatorID]map[[32]byte]struct{}),
		peerFirstOnion2b:   make(map[OperatorID]*Onion2b),
		rule1Fired:         make(map[int]map[OperatorID]bool, K),
		rule3LeaderFired:   make(map[int]map[OperatorID]bool, K),
		rule4Fired:         make(map[int]map[OperatorID]bool, K),
		rule5Fired:         make(map[int]map[OperatorID]bool, K),
		rule6bFired:          make(map[int]map[OperatorID]bool, K),
		evidenceObserved:     make(map[evidenceObservedKey]bool),
		l0VerdictReadyCh:     make(chan struct{}),
		l0SigmaEligibilityCh: make(chan struct{}),
	}, nil
}

// L0VerdictReadyCh returns a channel closed when the operator can determine
// their L_0 Phase-2a verdict per spec §Phase 2a / Verdict broadcast timing:
//   - uniquely-retained Phase-1 bundle at L_0 (non-auth-only) AND host has
//     returned a valid/not-valid verdict on it, OR
//   - ≥ 2 distinct V's retained at L_0 (leader equivocation → NR), OR
//   - operator is the L_0 leader and has built their own L_0 bundle.
//
// One-shot per slot. Callers select on this against the T_verdict_max-ε_proc
// fallback ticker.
func (i *Instance) L0VerdictReadyCh() <-chan struct{} { return i.l0VerdictReadyCh }

// L0SigmaEligibilityCh returns a channel closed when the operator's local
// verdict-pool at L_0 has ≥ qV σV verdicts for some V (the convergence rule
// will yield a stable σ output at L_0). One-shot per slot. Callers select on
// this against the T_commit+ε_proc fallback ticker for Phase-2b emit timing.
func (i *Instance) L0SigmaEligibilityCh() <-chan struct{} { return i.l0SigmaEligibilityCh }

// l0VerdictReady reports whether the operator's L_0 verdict is determinable.
//
// Unlike OBFT, 2abOBFT has no Phase-1 σ_V (Variant C) — the L_0 leader's
// own-bundle case is handled via the standard retention+host-validity path:
// the runner calls ObservePhase1Bundle on the leader's own bundle (which
// retains it) and ApplyHostValidity on the leader's V (which records the
// host verdict). Both fire maybeSignalL0VerdictReady; the predicate returns
// true once both events have landed.
func (i *Instance) l0VerdictReady() bool {
	const layer = 0
	leaderMap := i.retainedBundles[layer]
	if len(leaderMap) == 0 {
		return false
	}
	expectedLeader := i.cfg.Layers[layer].Leader
	retained := leaderMap[expectedLeader]
	if len(retained) >= 2 {
		// Equivocation observed → forced NR per cross-phase exclusivity.
		return true
	}
	if len(retained) != 1 {
		return false
	}
	// Auth-only entries don't drive the operator's own verdict per spec
	// §Phase 1 / "auth-only retention".
	if retained[0].AuthOnly {
		return false
	}
	verdicts := i.hostVerdict[layer]
	if verdicts == nil {
		return false
	}
	root := ValueRoot(retained[0].Bundle.Value)
	_, recorded := verdicts[string(root[:])]
	return recorded
}

// l0SigmaEligibilityReached reports whether the operator's local verdict-pool
// at L_0 has ≥ qV `σV` verdicts on some single V (so the convergence rule
// will yield a stable σ output at L_0 even without further verdict arrivals).
func (i *Instance) l0SigmaEligibilityReached() bool {
	const layer = 0
	peer := i.peerVerdicts[layer]
	own := i.ownVerdict[layer]
	if len(peer) == 0 && own == nil {
		return false
	}
	// Tally σV verdicts per value_root. Exclude operators who have been
	// flagged as verdict-equivocators (treat-equivocator-as-null SHOULD
	// rule, spec §Phase 2a). Skip the local op's own entry in the peer map
	// — own's σV is already tallied via `own` above; a runner that
	// self-observes its own verdict would otherwise double-count it.
	equivocators := i.verdictEquivocator[layer]
	counts := make(map[[32]byte]int)
	if own != nil && own.Kind == VerdictSigmaV {
		counts[own.ValueRoot]++
	}
	for op, v := range peer {
		if op == i.ownOperatorID {
			continue
		}
		if equivocators[op] {
			continue
		}
		if v.Kind != VerdictSigmaV {
			continue
		}
		counts[v.ValueRoot]++
	}
	qV := 2*i.cfg.F + 1
	for _, c := range counts {
		if c >= qV {
			return true
		}
	}
	return false
}

// maybeSignalL0VerdictReady closes l0VerdictReadyCh if newly satisfied.
// Safe to call repeatedly; subsequent calls are no-ops. Caller holds the
// instance mutex.
func (i *Instance) maybeSignalL0VerdictReady() {
	select {
	case <-i.l0VerdictReadyCh:
		return
	default:
	}
	if i.l0VerdictReady() {
		close(i.l0VerdictReadyCh)
	}
}

// maybeSignalL0SigmaEligibility closes l0SigmaEligibilityCh if newly
// satisfied. Safe to call repeatedly. Caller holds the instance mutex.
func (i *Instance) maybeSignalL0SigmaEligibility() {
	select {
	case <-i.l0SigmaEligibilityCh:
		return
	default:
	}
	if i.l0SigmaEligibilityReached() {
		close(i.l0SigmaEligibilityCh)
	}
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

// Evidence returns a shallow-snapshot copy of accumulated evidence
// entries. Order is insertion order.
//
// SHALLOW: the slice itself is freshly allocated, but each Evidence's
// typed-payload pointer fields (CrossSigning, LeaderEquivocation, ...)
// alias the same backing structs stored inside the Instance. Callers
// MUST treat the returned entries (and their payloads) as read-only;
// mutating a payload field corrupts internal state. Loggers/persisters
// should read-only-marshal the entries; if a mutable copy is needed,
// the caller deep-copies after snapshot.
//
// Safe to call concurrently with the Instance only if the caller
// serializes access.
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

// ---------- Phase-2b EKM coordination ----------

// transitionToSigma applies the σ-emit EKM lock for `layer` on `value`.
// Returns ErrSigmaLocked if already locked on a different V (single-σ-V
// invariant); ErrNRLocked if the operator already NR-committed at this
// layer (σ-XOR-NR invariant). Idempotent on (layer, value).
func (i *Instance) transitionToSigma(layer int, value Value) error {
	if i.nrLocked[layer] {
		return ErrNRLocked
	}
	if i.sigmaLocked[layer] {
		if !bytes.Equal(i.sigmaLockedV[layer], value) {
			return ErrSigmaLocked
		}
		return nil
	}
	i.sigmaLocked[layer] = true
	i.sigmaLockedV[layer] = append(Value{}, value...)
	return nil
}

// transitionToNR applies the NR-emit EKM lock for `layer`. Returns
// ErrSigmaLocked if the operator already σ-committed at this layer
// (σ-XOR-NR invariant). Idempotent.
func (i *Instance) transitionToNR(layer int) error {
	if i.sigmaLocked[layer] {
		return ErrSigmaLocked
	}
	i.nrLocked[layer] = true
	return nil
}

// chainEncryptForLayer encrypts `partial` for layer `k` using the
// chained-IBE construction from spec §Phase 2b emission:
//
//	layer k:  E_{nr_tag_0}( ... E_{nr_tag_{k-1}}( σ_partial ) ... )
//
// The innermost wrap uses nr_tag_{k-1}; the outermost uses nr_tag_0.
// At k = 0, returns `partial` unchanged (plaintext per spec).
func (i *Instance) chainEncryptForLayer(k int, partial []byte) ([]byte, error) {
	if k == 0 {
		return partial, nil
	}
	inner := partial
	for j := k - 1; j >= 0; j-- {
		tag := NoQuorumTag(i.cfg.ClusterID, i.cfg.Height, j)
		ct, err := i.ibe.Encrypt(i.clusterPubKey, tag, inner)
		if err != nil {
			return nil, fmt.Errorf("twoab: encrypt at chain level %d: %w", j, err)
		}
		inner = ct
	}
	return inner, nil
}

// chainDecryptForLayer decrypts a layer-`k` ciphertext using
// `decryptionKeys` where decryptionKeys[j] is the aggregated NR-partials
// signature on nr_tag_j. Used by Phase H (reconstruction); included here
// for completeness alongside chainEncryptForLayer.
//
// Decryption applies keys outermost-first.
func (i *Instance) chainDecryptForLayer(k int, ciphertext []byte, decryptionKeys [][]byte) ([]byte, error) {
	if k == 0 {
		return ciphertext, nil
	}
	if len(decryptionKeys) < k {
		return nil, fmt.Errorf("twoab: need %d chained-decryption keys for layer %d, have %d",
			k, k, len(decryptionKeys))
	}
	outer := ciphertext
	for j := 0; j < k; j++ {
		pt, err := i.ibe.Decrypt(outer, decryptionKeys[j])
		if err != nil {
			return nil, fmt.Errorf("twoab: decrypt at chain level %d: %w", j, err)
		}
		outer = pt
	}
	return outer, nil
}

// recordRule1 marks Rule 1 (CrossSigning) as fired for (op, layer).
// Returns true if this is the first observation; false if Rule 1 was
// already recorded.
func (i *Instance) recordRule1(op OperatorID, layer int) bool {
	if i.rule1Fired[layer] == nil {
		i.rule1Fired[layer] = make(map[OperatorID]bool)
	}
	if i.rule1Fired[layer][op] {
		return false
	}
	i.rule1Fired[layer][op] = true
	return true
}

// recordRule3Leader marks Rule 3 leader-extension as fired for (op, layer).
// Used by ObserveOnion2b's σ-side branch at L_0 when a leader's onion σ
// differs from their retained Phase-1 V — though in 2ab the bundle has
// no σ_V, this check fires when the onion's claimed V differs from any
// retained V from the same leader at L_0 (the bare-OBFT analog).
func (i *Instance) recordRule3Leader(op OperatorID, layer int) bool {
	if i.rule3LeaderFired[layer] == nil {
		i.rule3LeaderFired[layer] = make(map[OperatorID]bool)
	}
	if i.rule3LeaderFired[layer][op] {
		return false
	}
	i.rule3LeaderFired[layer][op] = true
	return true
}

// recordRule4 marks Rule 4 (fake encrypted-presence at k > 0) as fired
// for (op, layer). Returns true if this is the first observation; false
// if Rule 4 was already recorded.
func (i *Instance) recordRule4(op OperatorID, layer int) bool {
	if i.rule4Fired[layer] == nil {
		i.rule4Fired[layer] = make(map[OperatorID]bool)
	}
	if i.rule4Fired[layer][op] {
		return false
	}
	i.rule4Fired[layer][op] = true
	return true
}

// recordRule5Fired marks Rule 5 (fake plaintext σ at L_0 — either
// cryptoFake or unknownV) as fired for (op, layer). Returns true if this
// is the first observation; false if Rule 5 was already recorded.
func (i *Instance) recordRule5Fired(op OperatorID, layer int) bool {
	if i.rule5Fired[layer] == nil {
		i.rule5Fired[layer] = make(map[OperatorID]bool)
	}
	if i.rule5Fired[layer][op] {
		return false
	}
	i.rule5Fired[layer][op] = true
	return true
}

// recordRule6b marks Rule 6b verdict-vs-action as fired for (op, layer).
func (i *Instance) recordRule6b(op OperatorID, layer int) bool {
	if i.rule6bFired[layer] == nil {
		i.rule6bFired[layer] = make(map[OperatorID]bool)
	}
	if i.rule6bFired[layer][op] {
		return false
	}
	i.rule6bFired[layer][op] = true
	return true
}
