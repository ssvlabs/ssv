package twoab

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
)

// Instance is the per-slot 2abOBFT consensus state machine.
//
// Lifecycle (per docs/2abOBFT.md §Slot structure):
//
//  1. Phase 1 — [slot_start, T_0_broadcast]:
//     a. If local operator is a layer leader: BuildPhase1Bundle(layer, V)
//     constructs a bundle and broadcasts it. Every layer leader embeds an
//     LeaderSigma (σ partial on V at its layer) so receivers can seed
//     σ-pool[V_k] from the bundle alone (every layer's leader carries a
//     witness, not just L_0).
//     b. ObservePhase1Bundle(b) for every retained peer bundle.
//
//  2. Phase 2a — fire-instant at T_phase_2a:
//     a. MaybeFirePhase2a() — every operator emits exactly one of
//     {KindValue, KindNoValue, KindCommit-NRDirect} per their local state
//     (retention + host validity + L_0 equivocation observation).
//     b. ObserveValueMsg / ObserveNoValueMsg / ObserveCommit for peers.
//
//  3. Phase 2a-late — opportunistic upgrade window [T_phase_2a, slot deadline]:
//     a. MaybeBuildAndBroadcastUpgrade() — KindNoValue-path ops who have
//     received V_0 + host valid emit an upgrade KindValue (the upgrade).
//
//  4. Phase 2b — dynamic, no protocol-level deadline:
//     a. MaybeBuildAndBroadcastCommit() — each op evaluates the
//     NR-eligibility trigger (cluster noValuePool[L_0] reaches qEnc AND
//     cannot-σ gate); fires at most one KindCommit-NR per slot. The
//     equivocation trigger fires at Phase 2a (NRDirect) only; there is
//     no separate σ-eligibility trigger — KindValue is the σ-side
//     terminal emission with the emitter's σ partial inline.
//     b. ObserveCommit(c) for peers (Side ∈ {NR, NRDirect}).
//
//  5. Phase 3 — observer-on-arrival from Phase-2a onward:
//     a. Resolve() walks layers 0..K-1 checking σ-quorum / NR-quorum;
//     returns Output (success) or ErrNoQuorum.
//     b. On success: BuildCertificate(out) → broadcast for downstream peers.
//     c. ObserveCertificate(c) — fallback submission path.
//
// Instance is NOT thread-safe; callers must serialize access. The
// expected SSV adapter runs each Instance behind its own mutex.
//
// Per spec §Emission ordering, on every state delta (after each Observe*)
// the Instance internally invokes:
//  1. MaybeBuildAndBroadcastUpgrade — upgrade check first.
//  2. MaybeBuildAndBroadcastCommit — then trigger evaluation.
//
// Callers don't invoke these directly during the slot; the Instance does
// it automatically. Manual invocation (e.g., on a timer-driven slot
// deadline) is permitted but not required.

// MaxRetainedPerOpLayer caps the number of distinct Phase-1 bundles (by
// value_root) retained per (layer, leader) tuple. Per spec
// docs/2abOBFT.md §Phase 1 (Retention bounds): a third
// distinct V from the same leader is silently dropped — the first two
// already establish equivocation evidence (Rule 2) and the third adds
// no protocol-level information beyond what's already on the wire.
//
// Mirrors `protocol/v2/obft/base.MaxRetainedPerOpLayer` in NAME and
// VALUE. The cross-package semantic surface is slightly different
// though: base applies the same cap to BOTH Phase-1 bundle retention
// AND σ-onion entry retention; twoab applies it only to Phase-1 bundle
// retention (twoab's σ partial lives directly in KindValue.L0Partial,
// not in a separate σ-onion message). Documented in
// docs/OBFT-TWOAB-CONVERGENCE-PLAN.md §L5.
const MaxRetainedPerOpLayer = 2

type Instance struct {
	cfg           *Config
	ownOperatorID OperatorID

	// Cryptographic dependencies — declared at construction time.
	signer          obft.Signer
	tagSigner       obft.Signer
	ibe             obft.ThresholdIBE
	clusterPubKey   []byte
	pubKeyShares    map[OperatorID][]byte
	ibePubKeyShares map[OperatorID][]byte // optional under Option A

	// retainedBundles[layer][leader_id] = bundles retained for this
	// (layer, leader). Capped at 2 distinct value_roots per spec §Phase 1
	// Retention bounds. In 2abOBFT there is no auth-only vs regular
	// retention distinction — all in-slot bundles are retained equivalently
	// (no T_commit hard wall; the slot deadline at the runner level is the
	// only hard cutoff).
	retainedBundles map[int]map[OperatorID][]*retainedBundle

	// hostVerdict[layer][value_root] = host application's
	// valid/not-valid verdict on the V identified by value_root at this
	// layer. Populated via ApplyHostValidity; consumed by
	// computeLocalValueState (Phase-2a fire-time emission decision),
	// canSigmaAtLayer (the can-σ self-gate), and
	// MaybeBuildAndBroadcastUpgrade (upgrade preconditions).
	hostVerdict map[int]map[[32]byte]bool

	// Phase-2a own emissions. ownNoValueMsg is set if the op emitted
	// NoValue at Phase 2a; ownValueMsg is set if the op emitted Value at
	// Phase 2a OR as a Phase-2a-late upgrade from a prior NoValue. Both
	// non-nil indicates the upgrade happened. Phase-2a NRDirect emitters
	// set ownCommit directly (Side=NRDirect) and leave ownValueMsg /
	// ownNoValueMsg nil.
	ownValueMsg   *ValueMsg
	ownNoValueMsg *NoValueMsg

	// Phase-2b own emission. At most one Commit per slot per operator,
	// per spec §Phase 2b. Set by MaybeBuildAndBroadcastCommit OR by
	// MaybeFirePhase2a (when ComputeLocalValueState returns NRDirect).
	ownCommit *Commit

	// Peer Phase-2 emissions — first-observed retained per (slot, op).
	// Used for:
	//   - Pool aggregation per §Pool aggregation rules (KindValue carries
	//     both the σ-direction claim AND the emitter's σ partial directly).
	//   - Duplicate / equivocation detection (second distinct emission =
	//     Phase-2 equivocation per Rule 6a, unless authorized by
	//     {NoValue→Value, NoValue→Commit-NR, Commit-NRDirect-alone}).
	peerValueMsg   map[OperatorID]*ValueMsg
	peerNoValueMsg map[OperatorID]*NoValueMsg
	peerCommit     map[OperatorID]*Commit

	// Claim pools per §Pool aggregation rules. Maintained incrementally on
	// every Observe* call. The pools are layer-indexed to support Phase 3
	// reconstruction at deeper layers (though Phase 2b Commit emission is
	// L_0-only; L_k>0 commitments are captured at Phase 2a inside
	// LayerEntries).
	//
	//   - valuePool[layer][V_root] = set of operators claiming σ-direction
	//     on V at this layer.
	//   - noValuePool[layer] = set of operators claiming NR-direction at
	//     this layer.
	//
	// Pool membership: an op's KindValue at L_0 adds them to
	// valuePool[0][ValueRoot]; their L_k>0 SigmaChained entry adds them
	// to valuePool[k][V_root]; their KindNoValue adds them to
	// noValuePool[0]; KindCommit-NR / KindCommit-NRDirect adds to
	// noValuePool[0].
	valuePool   map[int]map[[32]byte]map[OperatorID]bool
	noValuePool map[int]map[OperatorID]bool

	// Threshold pools (the actual σ and NR partials used for Phase 3
	// reconstruction). Distinct from claim pools — these only contain
	// extractable partial signatures, not inferred-claim memberships.
	//
	//   - sigmaPool[layer][V_root][op] = the op's σ partial on V at this
	//     layer. At L_0: extracted from KindValue.L0Partial via
	//     verifyAndPoolL0Partial; the leader's contribution comes from
	//     Phase1Bundle.LeaderSigma. At L_k>0: the layer leader's plaintext
	//     Phase1Bundle.LeaderSigma (a head-start) plus peers' σ partials
	//     decrypted from Phase-2a LayerEntry (SigmaChained, peeled via
	//     accumulated nr_tag keys).
	//   - nrTagPool[layer][op] = the op's nr_tag_k partial at this layer.
	//     At L_0: extracted from KindCommit-NR / KindCommit-NRDirect. At
	//     L_k>0: extracted from Phase-2a LayerEntry (NRPlaintext).
	//
	// Threshold pools are populated lazily as the Phase 3 walk needs
	// them (L_0 sigmaPool is populated on KindValue observation;
	// L_k>0 is populated during Resolve()'s chain-decryption walk once
	// enough nr_tag partials have aggregated to unlock the layer).
	sigmaPool map[int]map[[32]byte]map[OperatorID]Signature
	nrTagPool map[int]map[OperatorID]Signature

	// Phase-2b local commitment state — per-layer σ/NR lock + cached σ
	// partials. EKM enforces single-σ-V and σ-XOR-NR per (slot, layer)
	// at sign time.
	sigmaLocked  []bool
	sigmaLockedV []Value // when sigmaLocked[k], sigmaLockedV[k] is the V signed
	nrLocked     []bool
	ownPartials  map[int]Signature // cached σ partials per layer

	// State flags. phase2aFired ensures MaybeFirePhase2a fires at most
	// once per slot. (Per-tick processing — MaybeBuildAndBroadcastUpgrade
	// + MaybeBuildAndBroadcastCommit — is idempotent and may be re-invoked
	// on every state delta.)
	phase2aFired bool

	// l0ReadyCh is closed (once) when the op's L_0 Phase-2a emission
	// becomes determinable as σ-eligible (KindValue) or equivocation-
	// observed (KindCommit-NRDirect) — i.e. when computeLocalValueState()
	// would return Value or NRDirect. The runner/DES selects on it to
	// fire MaybeFirePhase2a async, well before the TPhase2a backstop,
	// shaving the bundle-arrival→TPhase2a gap off the healthy path.
	// Mirrors `protocol/v2/obft/base.Instance.l0ReadyCh`.
	//
	// SEMANTIC DIVERGENCE FROM base: base closes L0Ready on ANY host
	// verdict (NV and σ are both commitments in bare OBFT). twoab does
	// NOT close it for the NoValue case — a V-drop / host-NV op waits
	// for the TPhase2a backstop (giving V its reflood window before the
	// op declares NV), then emits KindNoValue; a later V arrival is
	// handled by the upgrade cascade. This is protocol-forced:
	// twoab's KindNoValue is coordination, not commitment.
	//
	// Stays open all slot if L_0 never becomes fire-ready (silent leader
	// / grossly-late bundle); NOT closed by Finalize (only
	// wantsHostValidationCh is). See l0DecisionReady / maybeSignalL0Ready.
	l0ReadyCh chan struct{}

	// Per-rule dedup buckets — ensure one Evidence entry per logical
	// fault even when multiple detection paths fire.
	rule1Fired  map[int]map[OperatorID]bool // Rule 1 cross-signing per (layer, op)
	rule3Fired  map[int]map[OperatorID]bool // Rule 3 per (layer, op)
	rule4Fired  map[int]map[OperatorID]bool // Rule 4 fake encrypted-presence per (layer, op)
	rule5Fired  map[int]map[OperatorID]bool // Rule 5 (cryptoFake or unknownV) per (layer, op)
	rule6aFired map[OperatorID]bool         // Rule 6a Phase-2 equivocation per op (slot-wide)

	// cascadeErrors accumulates errors returned by
	// MaybeBuildAndBroadcastUpgrade / MaybeBuildAndBroadcastCommit during
	// afterStateDelta cascades. These are otherwise-silent failures —
	// e.g. signer infra issues during commit/upgrade build that prevent
	// an emission from firing. Surfaced via CascadeErrors() so callers
	// can detect a silent BLS hiccup eating the cascade (otherwise
	// invisible until the slot misses for unclear reason).
	//
	// ErrUpgradeNotAvailable from MaybeBuildAndBroadcastUpgrade is the
	// no-op sentinel (preconditions not met) and is NOT recorded here;
	// only genuine signer/EKM failures accumulate.
	cascadeErrors []error

	// cascadeErrorsCapped is set true when recordCascadeError hit the
	// cap and dropped at least one error. Surfaced via Stats() so
	// callers can detect a runaway-signer pathological case (otherwise
	// the silent truncation would mask the failure-rate signal).
	cascadeErrorsCapped bool

	// host is the channel + dedup plumbing for peer-reflood-V host-validation
	// requests harvested from peer KindValues (see requestHostValidation /
	// maybeHarvestPhase1BundleFromValueMsg). The runner drains its channel and
	// calls back via ApplyHostValidity (which clears the pending flag). Closed
	// by Finalize.
	host *obft.HostValidationGate

	// verifiedWitnesses caches the result of verifySigmaPartial for a
	// layer's leader on a given (layer, V_root). Every KindValue forwards
	// leader witnesses, so a receiver observing N peer KindValues for the
	// same V in a slot would naively re-verify the same (leader, V_root)
	// witness N times. BLS partial-verify is ~1ms in production; at n=10
	// with byz-driven re-broadcast, the verify cost stacks against the
	// slot budget. The cache short-circuits subsequent verifies of the
	// same witness (verify-cost dedup); mirrors
	// `protocol/v2/obft/base/instance.go` witnessedLeaderSigma.
	//
	// Cache is per-layer: every layer's forwarded leader witness gets its
	// own (V_root) dedup bucket.
	verifiedWitnesses map[int]map[[32]byte]bool

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

	ended bool

	// lastResolveTrace mirrors obft/base's same-named field — per-layer
	// Resolve walk state, overwritten on each Resolve call. See
	// obft/base.Instance.lastResolveTrace and LastResolveLayerAttempts
	// for the semantics; consumed by consensustest's bucket-3
	// walk-consistency invariant.
	lastResolveTrace []LayerAttempt

	// verifiedPartials caches "σ partial X was BLS-verified successfully" so
	// Resolve can skip the redundant re-verify in its L_k>0 σ-walk (audit
	// finding F1; mirrors obft/base/Instance.verifiedPartials). twoab's
	// Resolve at L_0 reads from sigmaPool (already verified at observation),
	// so the cache only helps at L_k>0 where the chained-encrypted partial
	// is first decrypted and verified inside Resolve. On opportunistic re-
	// Resolve calls walking the same L_k>0 entries, cache hits skip the
	// BLS verify.
	//
	// Populate sites:
	//   - phase3.go aggregatePeerLayerEntries / similar L_k>0 walk — populate
	//     on first-time successful post-decrypt verify (via verifyOrCached).
	//
	// Safety contract identical to base: cache populate is gated EXCLUSIVELY
	// by "signer.VerifyPartial just returned true on this (value, partial)
	// pair". Cache key is (op, layer, sha256(value), sha256(partial)) — both
	// roots are load-bearing; partialRoot disambiguates byzantine
	// equivocation, valueRoot blocks the cross-V leakage attack where a
	// byzantine emits two entries with the same decrypted partial bytes but
	// different claimed V's. See verifyCacheKey doc-comment and
	// docs/OBFT-F1-F5-IMPLEMENTATION-PLAN.md for the full argument.
	//
	// Single-threaded by the controller's r.instanceMu (audit Q-Open-2), so
	// the map needs no internal synchronisation.
	verifiedPartials map[verifyCacheKey]struct{}
}

// verifyCacheKey identifies a unique (op, layer, value, partial) tuple for
// the verifiedPartials cache. Mirrors obft/base.verifyCacheKey exactly —
// see that type's doc-comment for the safety argument. Both valueRoot and
// partialRoot are load-bearing:
//
//   - partialRoot makes byzantine equivocation safe (distinct partials at
//     the same (op, layer, value) cache independently).
//   - valueRoot blocks cross-V leakage: byzantine emits entry A
//     (Value=V_a, Ciphertext=enc(σ_a)) and entry B (Value=V_b, Ciphertext'
//     also decrypts to σ_a). Without valueRoot, A's cache populate would
//     let B cache-hit and σ_a would incorrectly contribute to V_b's pool.
type verifyCacheKey struct {
	op          OperatorID
	layer       int
	valueRoot   [32]byte
	partialRoot [32]byte
}

// RetentionSource discriminates how a retained Phase-1 bundle reached
// the Instance. Informational hint for slashing-evidence routing: the
// LeaderSigma BLS partial is sufficient cryptographic leader-binding in
// all cases (the Instance verifies it before retention), so envelope
// re-verification by downstream consumers is OPTIONAL — useful as a
// belt-and-suspenders check when available, skippable when not.
// Source surfaces availability rather than necessity.
type RetentionSource int

const (
	// RetentionDirect: the bundle reached the Instance via a direct
	// ObservePhase1Bundle call. The leader's envelope signature is
	// available at the wire layer (in the runner's mcache); downstream
	// consumers MAY re-verify the envelope against the leader's pubkey
	// as a belt-and-suspenders check on top of LeaderSigma verification.
	RetentionDirect RetentionSource = iota
	// RetentionHarvest: the bundle was synthesized from a peer's
	// KindValue (peer-reflood-V harvest). No leader envelope
	// signature exists for this bundle; the LeaderSigma BLS partial
	// inside is the sole leader-binding artifact (verified by the
	// Instance against the leader's pubkey before retention).
	// Downstream consumers should skip envelope re-verification (none
	// to re-verify) and rely on LeaderSigma alone.
	RetentionHarvest
)

// retainedBundle wraps a Phase-1 bundle. In 2abOBFT there is no
// auth-only vs regular retention distinction — all in-slot bundles
// are retained equivalently (no T_commit hard wall).
type retainedBundle struct {
	// Bundle is a deep copy of the retained bundle (defensive against
	// caller-owned slice mutation post-Observe).
	Bundle *Phase1Bundle

	// RetentionEstablishedAt is the offset (from slot_start) at which
	// this retention entry was first observed. Diagnostic / telemetry
	// only — the protocol's behavior doesn't depend on it (there is no
	// T_commit acceptance horizon). Useful for runner-level latency
	// metrics and post-mortem analysis of mesh-tail recovery.
	RetentionEstablishedAt time.Duration

	// Source distinguishes Direct (envelope-signed Phase-1 bundle from
	// gossipsub Phase-1 channel) from Harvest (synthesized from a
	// peer's KindValue). Surfaced into Rule 2 (leader-equivocation)
	// evidence so downstream slashing consumers can route envelope
	// re-verification correctly. See RetentionSource.
	Source RetentionSource
}

// NewInstance constructs a 2abOBFT Instance. Validates the config and
// pubKeyShares at construction.
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
	if len(clusterPubKey) == 0 {
		return nil, errors.New("twoab: empty clusterPubKey (IBE trust anchor required)")
	}
	if !obft.OperatorInCluster(ownOperatorID, cfg.Operators) {
		return nil, fmt.Errorf("twoab: own operator id %d not in cluster", ownOperatorID)
	}
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
		cfg:               cfg,
		ownOperatorID:     ownOperatorID,
		signer:            signer,
		tagSigner:         tagSigner,
		ibe:               ibe,
		clusterPubKey:     clusterPubKey,
		pubKeyShares:      pubKeyShares,
		ibePubKeyShares:   ibePubKeyShares,
		evidenceObserver:  evidenceObserver,
		retainedBundles:   make(map[int]map[OperatorID][]*retainedBundle, K),
		hostVerdict:       make(map[int]map[[32]byte]bool, K),
		peerValueMsg:      make(map[OperatorID]*ValueMsg, len(cfg.Operators)),
		peerNoValueMsg:    make(map[OperatorID]*NoValueMsg, len(cfg.Operators)),
		peerCommit:        make(map[OperatorID]*Commit, len(cfg.Operators)),
		valuePool:         make(map[int]map[[32]byte]map[OperatorID]bool, K),
		noValuePool:       make(map[int]map[OperatorID]bool, K),
		sigmaPool:         make(map[int]map[[32]byte]map[OperatorID]Signature, K),
		nrTagPool:         make(map[int]map[OperatorID]Signature, K),
		sigmaLocked:       make([]bool, K),
		sigmaLockedV:      make([]Value, K),
		nrLocked:          make([]bool, K),
		ownPartials:       make(map[int]Signature, K),
		rule1Fired:        make(map[int]map[OperatorID]bool, K),
		rule3Fired:        make(map[int]map[OperatorID]bool, K),
		rule4Fired:        make(map[int]map[OperatorID]bool, K),
		rule5Fired:        make(map[int]map[OperatorID]bool, K),
		rule6aFired:       make(map[OperatorID]bool, len(cfg.Operators)),
		evidenceObserved:  make(map[evidenceObservedKey]bool),
		host:              obft.NewHostValidationGate(K),
		verifiedWitnesses: make(map[int]map[[32]byte]bool, K),
		l0ReadyCh:         make(chan struct{}),
		// Verify-cache capacity: bounded by n ops × K layers × small slack
		// for byzantine equivocation. Cheap upper bound; map is GC'd at
		// slot end so unbounded growth under attack isn't a concern.
		verifiedPartials: make(map[verifyCacheKey]struct{}, 2*K*len(cfg.Operators)),
	}, nil
}

// markVerified records that (op, layer, value, partial) has been BLS-verified
// successfully, so subsequent re-verifies in Resolve can skip the call. Both
// value and partial participate in the cache key — value-binding is load-
// bearing safety. See verifyCacheKey doc-comment for the full contract.
//
// MUST be called only from sites that just observed signer.VerifyPartial
// return true on the same (pub-share-for-op, value, partial) tuple Resolve
// will see later. Mirrors obft/base.Instance.markVerified.
func (i *Instance) markVerified(op OperatorID, layer int, value, partial []byte) {
	i.verifiedPartials[verifyCacheKey{
		op:          op,
		layer:       layer,
		valueRoot:   sha256.Sum256(value),
		partialRoot: sha256.Sum256(partial),
	}] = struct{}{}
}

// alreadyVerified reports whether (op, layer, value, partial) has been BLS-
// verified before via markVerified. Cache HIT lets Resolve skip a redundant
// signer.VerifyPartial call; MISS falls through to the full verify.
func (i *Instance) alreadyVerified(op OperatorID, layer int, value, partial []byte) bool {
	_, ok := i.verifiedPartials[verifyCacheKey{
		op:          op,
		layer:       layer,
		valueRoot:   sha256.Sum256(value),
		partialRoot: sha256.Sum256(partial),
	}]
	return ok
}

// verifyOrCached returns true if (op, layer, value, partial) is known-
// verified — either a cache hit (a prior code path already BLS-verified
// this exact (value, partial) pair), or a fresh signer.VerifyPartial that
// just succeeded (in which case the cache is populated for next time).
// Returns false ONLY when a fresh BLS verify ran and rejected the partial.
//
// Sole Resolve-side helper for the F1 cache; phase3.go's L_k>0 walk calls
// this in place of i.signer.VerifyPartial. The false-return path preserves
// existing Rule-4 / Rule-5 evidence handling — verifyOrCached doesn't touch
// evidence.
func (i *Instance) verifyOrCached(op OperatorID, layer int, pubShare, value, partial []byte) bool {
	if i.alreadyVerified(op, layer, value, partial) {
		return true
	}
	if !i.signer.VerifyPartial(pubShare, value, partial) {
		return false
	}
	i.markVerified(op, layer, value, partial)
	return true
}

// WantsHostValidationCh returns the channel on which the Instance delivers
// host-validity requests for V's first-observed via peer KindValue (the
// peer-reflood-V harvest path). The runner MUST drain this channel
// (typically via select alongside its other per-slot signals) and dispatch
// validation through the host hook, invoking ApplyHostValidity with the
// verdict.
//
// Direct Phase-1 bundle observation does NOT emit on this channel — the
// runner already runs ApplyHostValidity inline at bundle arrival
// (`evtPhase1Arrival` in the adapter). The channel exists only for the
// harvest path, where retention is established without a direct bundle
// observation and the host hasn't been queried for the harvested V.
//
// Buffered with capacity K; the Instance drops enqueue attempts if the
// buffer is full (back-pressure: a slow runner that isn't draining means
// the validation path is degraded, but the Instance never blocks). The
// channel is closed by Finalize.
func (i *Instance) WantsHostValidationCh() <-chan ValidationRequest {
	return i.host.Channel()
}

// L0ReadyCh returns a channel closed once when the operator's L_0
// Phase-2a emission becomes determinable as σ-eligible (KindValue) or
// equivocation-observed (KindCommit-NRDirect) — see l0DecisionReady.
// The runner/DES selects on it to fire MaybeFirePhase2a async,
// before the TPhase2a backstop. Mirrors
// `protocol/v2/obft/base.Instance.L0ReadyCh`, with the twoab semantic
// divergence documented on the l0ReadyCh field: the NoValue case does
// NOT close this channel (V-drop / host-NV ops wait for the TPhase2a
// backstop). Stays open all slot if L_0 never becomes fire-ready.
//
// Best-effort hint, NOT a binding contract: because ApplyHostValidity
// tolerates a valid→NV verdict flip (see its docstring), L0Ready is
// non-monotone in principle — it can close (op was Value/NRDirect) and
// then the determining state can change before the consumer fires (e.g.
// host flips V_0 to NV, so MaybeFirePhase2a would now emit KindNoValue).
// Consumers MUST treat "L0Ready closed" as "fire now" and re-derive the
// actual emission kind from MaybeFirePhase2a at fire time — never assume
// the kind from the channel close alone. (Unreachable in the DES, whose
// Host.Validate is a deterministic function of (op, layer, V); relevant
// only for a production runner that re-queries the host mid-slot.)
func (i *Instance) L0ReadyCh() <-chan struct{} { return i.l0ReadyCh }

// l0DecisionReady reports whether the op's L_0 Phase-2a emission is
// determinable as a σ-side (Value) or equivocation (NRDirect) emission
// — the two cases that fire early on L0Ready. Predicate underlying
// L0ReadyCh.
//
// Returns false for the NoValue case (no V retained, or host says NV,
// or host not yet consulted): those ops wait for the TPhase2a backstop
// so V has its full reflood window before they declare NV. This is the
// deliberate divergence from base's l0DecisionReady (which fires on any
// host verdict) — twoab's KindNoValue is coordination, not commitment.
//
// Accepted trade-off (see docs/2abOBFT.md §Phase 2a, Async fire on L0Ready): firing
// on the first retained V σ-locks before a second equivocating V can
// arrive. Under jittery delivery this changes the Equivocate_AllNR
// outcome from "always fall through to L_1" to "mostly decide fast at
// L_0, ~23% miss". Safety-preserving (Pigeonhole 2 still bounds two-V
// σ-quorum); the trade is equivocation-recovery for healthy-path
// latency. Tracked by TestAdapter_Equivocate_AllNR_JitterTradeoff.
func (i *Instance) l0DecisionReady() bool {
	return i.computeLocalValueState() != localValueStateNoValue
}

// maybeSignalL0Ready closes l0ReadyCh if the L_0 emission has just
// become determinable (Value or NRDirect). Safe to call repeatedly;
// close-at-most-once via the non-blocking drain check. Mirrors base's
// maybeSignalL0Ready.
//
// Called from the two — and only two — mutators of the inputs that
// computeLocalValueState reads (retainedBundles + hostVerdict):
//   - retainPhase1Bundle (via ObservePhase1Bundle direct + harvest)
//     — retention count change.
//   - ApplyHostValidity — host verdict recorded.
//
// NOTE: BuildPhase1Bundle does NOT call this (and need not): in twoab it
// σ-locks but does not self-retain, so computeLocalValueState (which is
// retention-driven) is unchanged by it. The leader's L0Ready closes when
// it self-observes its own bundle (ObservePhase1Bundle + ApplyHostValidity),
// which the DES/runner does at fetch time. This differs from base, whose
// l0DecisionReady checks sigmaLocked[0] and so signals from BuildPhase1Bundle.
func (i *Instance) maybeSignalL0Ready() {
	select {
	case <-i.l0ReadyCh:
		return // already closed
	default:
	}
	if i.l0DecisionReady() {
		close(i.l0ReadyCh)
	}
}

// requestHostValidation enqueues a host-validity request on the wants
// channel if (a) host hasn't already validated this V at this layer, and
// (b) no in-flight request exists for the same (layer, value_root).
// Non-blocking: drops on full buffer with rollback of the pending flag.
//
// Mirrors `protocol/v2/obft/base.Instance.requestHostValidation`.
//
// Post-Finalize safety: Finalize closes the wants channel, and a send on a
// closed channel panics even under `select`/default. Passing `i.ended` to
// host.Request makes the enqueue a no-op once Finalize has run, so the send
// is never attempted. This is a defensive backstop — the only path that
// reaches here is ObserveValueMsg → harvest, and every Observe* method
// returns ErrInstanceEnded post-Finalize, so this function is already
// unreachable after the channel closes. Finalize sets i.ended before
// host.Close(), so there is no window where the channel is closed while
// i.ended is still false.
func (i *Instance) requestHostValidation(layer int, value Value) {
	i.host.Request(layer, value, i.cfg.K(), i.ended, func(l int, root [32]byte) bool {
		verdicts := i.hostVerdict[l]
		if verdicts == nil {
			return false
		}
		_, recorded := verdicts[root]
		return recorded
	})
}

// Config returns the instance's config (read-only).
func (i *Instance) Config() *Config { return i.cfg }

// OwnOperatorID returns the local operator's ID.
func (i *Instance) OwnOperatorID() OperatorID { return i.ownOperatorID }

// LeaderAtLayers returns the layer indices where the local operator is
// the designated leader. Empty if the operator is not a leader at any
// layer in this slot.
//
// Consumed by the planned twoab SSV runner adapter at slot-start to
// enumerate which layers the local op needs to fetch + broadcast
// Phase-1 bundles for (the parallel `protocol/v2/ssv/runner/obft`
// adapter for bare-OBFT uses the identical helper on the base Instance;
// the twoab runner adapter is Phase L scope and not yet landed). Kept
// in the public API surface so the runner adapter doesn't need to plumb
// it through Config.Layers iteration at the call site.
func (i *Instance) LeaderAtLayers() []int {
	var out []int
	for k, l := range i.cfg.Layers {
		if l.Leader == i.ownOperatorID {
			out = append(out, k)
		}
	}
	return out
}

// InstanceStats is an introspection snapshot of internal counters that
// are otherwise unobservable through the public API. Stable across
// versions: fields are additive (new counters appended, never removed
// or renamed). Intended for tests, telemetry, and diagnostic tooling.
type InstanceStats struct {
	// PendingValidationCount is the total number of (layer, V_root)
	// pairs currently waiting on host-validation reply via
	// wantsHostValidationCh. Summed across all layers.
	PendingValidationCount int

	// VerifiedWitnessesCount is the total number of (layer, V_root)
	// entries cached in verifiedWitnesses (positive cache only — every
	// entry represents a successful BLS leader-witness verify that
	// subsequent harvests short-circuit).
	VerifiedWitnessesCount int

	// CascadeErrorsCapped is true if recordCascadeError dropped at
	// least one error due to hitting cascadeErrorsCap. Indicates a
	// pathological signer scenario.
	CascadeErrorsCapped bool

	// CascadeErrorsCount is the current accumulator length (≤ cap).
	CascadeErrorsCount int

	// EvidenceCount is the current evidence accumulator length.
	EvidenceCount int

	// Ended reflects whether Finalize has been called.
	Ended bool
}

// Stats returns an introspection snapshot of the Instance's internal
// counters. Read-only — callers receive a value-typed struct that's
// safe to inspect after the call returns. Stable additive contract:
// new fields may be appended; existing fields are not removed or
// renamed. Used by tests and telemetry to observe behavior that the
// per-method API doesn't surface (e.g., the buffer-full rollback's
// pendingValidation cleanup, the leader-witness verify-cost dedup cache).
func (i *Instance) Stats() InstanceStats {
	pending := i.host.PendingCount()
	verified := 0
	for _, bucket := range i.verifiedWitnesses {
		verified += len(bucket)
	}
	return InstanceStats{
		PendingValidationCount: pending,
		VerifiedWitnessesCount: verified,
		CascadeErrorsCapped:    i.cascadeErrorsCapped,
		CascadeErrorsCount:     len(i.cascadeErrors),
		EvidenceCount:          len(i.evidence),
		Ended:                  i.ended,
	}
}

// CascadeErrors returns a snapshot of errors accumulated during
// afterStateDelta cascades — silent failures of the cascade-driven
// MaybeBuildAndBroadcastUpgrade / MaybeBuildAndBroadcastCommit paths.
// Typically empty; non-empty indicates a signer/EKM infra failure that
// prevented an emission from firing.
//
// The ErrUpgradeNotAvailable sentinel is filtered out (it's the
// preconditions-not-met no-op, not a genuine failure).
func (i *Instance) CascadeErrors() []error {
	out := make([]error, len(i.cascadeErrors))
	copy(out, i.cascadeErrors)
	return out
}

// Evidence returns a shallow-snapshot copy of accumulated evidence
// entries. Order is insertion order.
//
// SHALLOW: the slice itself is freshly allocated, but each Evidence's
// typed-payload pointer fields alias the same backing structs stored
// inside the Instance. Callers MUST treat the returned entries (and
// their payloads) as read-only; mutating a payload field corrupts
// internal state.
func (i *Instance) Evidence() []Evidence {
	out := make([]Evidence, len(i.evidence))
	// Shallow copy: Evidence is a value type but its typed-payload fields
	// (CrossSigning, CrossOnion, etc.) are pointers shared with the
	// Instance's internal slice. Deep-copying every payload on every read
	// is wasteful; the read-only contract above keeps this safe.
	copy(out, i.evidence)
	return out
}

// RetainedBundles returns the bundles retained at (layer, leader_id) at
// the moment of the call. Returns nil if no bundles are retained for
// this key.
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

// Finalize closes the Instance. Idempotent. Closes wantsHostValidationCh
// on first call so runners draining via range or select-with-zero-value
// can detect end-of-slot. Repeat calls are safe under the Instance's
// single-goroutine serialization contract (callers MUST serialize access
// per the package preamble) — the `i.ended` gate prevents the double-
// close-panic on sequential repeat calls. Concurrent Finalize calls
// from multiple goroutines are NOT safe and would race on the
// `i.ended = true` write versus the `close(...)` call; the type-level
// contract precludes this.
//
// Note: l0ReadyCh is NOT closed by Finalize. It's only ever closed by
// maybeSignalL0Ready when the L_0 emission becomes determinable; if
// that never happens (e.g., V never retained + host-valid), the channel
// stays open. Callers waiting on l0ReadyCh MUST bound their wait via
// ctx and/or the TPhase2a backstop timer — see RunProposerSlot's select.
func (i *Instance) Finalize() {
	if i.ended {
		return
	}
	i.ended = true
	i.host.Close()
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

// ---------- EKM coordination ----------

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
// chained-IBE construction from spec §Phase 2a / §Wire format:
//
//	layer k:  E_{nr_tag_0}( ... E_{nr_tag_{k-1}}( σ_partial ) ... )
//
// The innermost wrap uses nr_tag_{k-1}; the outermost uses nr_tag_0.
// At k = 0, returns `partial` unchanged (plaintext per spec).
func (i *Instance) chainEncryptForLayer(k int, partial []byte) ([]byte, error) {
	return obft.ChainEncryptForLayer(i.ibe, i.clusterPubKey, i.cfg.ClusterID, i.cfg.Height, k, partial)
}

// verifyNRTagPartial returns true if `partial` is a valid threshold
// signature share on nr_tag_layer for op. Used by ObserveCommit (NR /
// NRDirect L0Partial) and processObservedLayerEntries (L_k>0 NRPlaintext
// payloads) to gate NR contributions before they pollute the threshold
// pools. Without this verification, a byz could spam garbage NR
// partials, causing Phase-3 chain-decryption aggregation to either fail
// (StubSigner.AggregatePartials returns error) or produce a wrong
// derivation key (real-BLS Lagrange interpolation on garbage shares).
//
// IBE shares fallback (Option A vs Option B): when ibePubKeyShares is
// nil, falls back to pubKeyShares — the Option A integration where the
// validator's V-keypair shares double as IBE shares via DST separation
// in the BLS primitive. Option B (separate IBE-DKG) sets
// ibePubKeyShares to the IBE-derived shares; this code path then
// verifies against those instead. Mirrors base/phase2.go's
// verifyCommitNRPartials helper.
func (i *Instance) verifyNRTagPartial(op OperatorID, layer int, partial Signature) bool {
	nrShares := i.ibePubKeyShares
	if nrShares == nil {
		nrShares = i.pubKeyShares
	}
	pubShare, ok := nrShares[op]
	if !ok || len(pubShare) == 0 {
		return false
	}
	// Config.SkipNRPartialReverify gates the in-Instance repeat so the
	// production path — where message/validation/twoab_validation.go runs
	// Verifier.VerifyCommit/VerifyValueMsg/VerifyNoValueMsg before dispatch
	// — skips ~18 ms/slot of redundant BLS verifies. Callers that can't
	// guarantee the upstream verify (consensustest, ad-hoc test harnesses)
	// leave the flag at the default false. See the field's doc-comment in
	// config.go for the safety contract.
	if i.cfg.SkipNRPartialReverify {
		return true
	}
	tag := NoQuorumTag(i.cfg.ClusterID, i.cfg.Height, layer)
	return i.tagSigner.VerifyPartial(pubShare, tag, partial)
}

// chainDecryptForLayer decrypts a layer-`k` ciphertext using
// `decryptionKeys` where decryptionKeys[j] is the aggregated NR-partials
// signature on nr_tag_j. Used by Phase 3 (reconstruction).
//
// Decryption applies keys outermost-first.
func (i *Instance) chainDecryptForLayer(k int, ciphertext []byte, decryptionKeys [][]byte) ([]byte, error) {
	return obft.ChainDecryptForLayer(i.ibe, k, ciphertext, decryptionKeys)
}

// ---------- Per-rule dedup helpers ----------

// recordRulePerLayer is the shared template for per-(op, layer) evidence
// rules (Rules 1, 3, 4, 5 — all keyed by layer × op). Lazily initializes
// the inner per-op map at `table[layer]`, then marks (op, layer) as
// fired. Returns true on first observation, false if the rule had already
// been recorded for this (op, layer) pair.
//
// Rule 6a is intentionally NOT routed through this helper — it is
// slot-wide (per-op, not per-layer) and uses a flat map[OperatorID]bool;
// see recordRule6a.
func (i *Instance) recordRulePerLayer(table map[int]map[OperatorID]bool, op OperatorID, layer int) bool {
	if table[layer] == nil {
		table[layer] = make(map[OperatorID]bool)
	}
	if table[layer][op] {
		return false
	}
	table[layer][op] = true
	return true
}

// recordRule1 marks Rule 1 (CrossSigning) as fired for (op, layer).
// Returns true if this is the first observation; false if Rule 1 was
// already recorded.
func (i *Instance) recordRule1(op OperatorID, layer int) bool {
	return i.recordRulePerLayer(i.rule1Fired, op, layer)
}

// recordRule3 marks Rule 3 (cross-commit equivocation) as fired for
// (op, layer). Returns true if this is the first observation.
func (i *Instance) recordRule3(op OperatorID, layer int) bool {
	return i.recordRulePerLayer(i.rule3Fired, op, layer)
}

// maybeFireCrossSigmaV fires Rule 3 (cross-σ-V equivocation) if `op` now
// holds verified σ partials on ≥ 2 distinct value-roots at L_0, having just
// contributed `justV`. Called after every L_0 σ-pool insertion — the leader's
// Phase-1 LeaderSigma, an emitter's own KindValue.L0Partial, and harvested
// forwarded witnesses — so a cross-SOURCE double-sign is caught in any arrival
// order (e.g. a byz leader's witness on V_a plus its own L0Partial on V_b).
// The two-distinct-KindValues case is caught separately in ObserveValueMsg;
// recordRule3's per-(op, layer) dedup keeps the two paths from double-firing.
//
// L_0-only: deeper-layer σ partials are chained-encrypted (not plaintext), so
// no cross-source plaintext comparison exists there. All pooled partials are
// already BLS-verified (pool inclusion gates on verify), so two pool entries
// for one op on distinct roots is cryptographic proof of cross-σ-V.
func (i *Instance) maybeFireCrossSigmaV(op OperatorID, justV Value) {
	const layer = 0
	pools := i.sigmaPool[layer]
	justRoot := ValueRoot(justV)
	if _, ok := pools[justRoot][op]; !ok {
		return // the just-pooled partial isn't present (defensive)
	}
	for otherRoot, opPartials := range pools {
		if otherRoot == justRoot {
			continue
		}
		if _, ok := opPartials[op]; !ok {
			continue
		}
		// op has σ partials on two distinct value-roots at L_0.
		if !i.recordRule3(op, layer) {
			return // already recorded (e.g. via the two-KindValues path)
		}
		otherV, _ := i.recoverV(layer, otherRoot)
		// Deterministic A/B ordering by value-root for stable evidence.
		vA, pA, vB, pB := justV, pools[justRoot][op], otherV, pools[otherRoot][op]
		if bytes.Compare(otherRoot[:], justRoot[:]) < 0 {
			vA, pA, vB, pB = otherV, pools[otherRoot][op], justV, pools[justRoot][op]
		}
		i.recordEvidence(Evidence{
			Rule:       EvidenceCrossCommitEquivocation,
			OperatorID: op,
			Layer:      layer,
			CrossCommitEquivocation: &CrossCommitEquivocationEvidence{
				ValueA:   append(Value{}, vA...),
				ValueB:   append(Value{}, vB...),
				PartialA: append(Signature{}, pA...),
				PartialB: append(Signature{}, pB...),
			},
		})
		return
	}
}

// recordRule4 marks Rule 4 (fake encrypted-presence at k > 0) as fired
// for (op, layer). Returns true if this is the first observation.
func (i *Instance) recordRule4(op OperatorID, layer int) bool {
	return i.recordRulePerLayer(i.rule4Fired, op, layer)
}

// recordRule5 marks Rule 5 (fake plaintext σ at L_0) as fired for
// (op, layer). Returns true if this is the first observation.
func (i *Instance) recordRule5(op OperatorID, layer int) bool {
	return i.recordRulePerLayer(i.rule5Fired, op, layer)
}

// recordRule6a marks Rule 6a (Phase-2 equivocation) as fired for op.
// Rule 6a is slot-wide (not per-layer) — the offending sequence is a
// pair (or triple) of Phase-2 emissions from the same op within the
// slot. Returns true if this is the first observation.
func (i *Instance) recordRule6a(op OperatorID) bool {
	if i.rule6aFired[op] {
		return false
	}
	i.rule6aFired[op] = true
	return true
}

// ---------- Pool maintenance helpers ----------

// addToValuePool adds `op` to valuePool[layer][vRoot]. Idempotent.
func (i *Instance) addToValuePool(layer int, vRoot [32]byte, op OperatorID) {
	if i.valuePool[layer] == nil {
		i.valuePool[layer] = make(map[[32]byte]map[OperatorID]bool)
	}
	if i.valuePool[layer][vRoot] == nil {
		i.valuePool[layer][vRoot] = make(map[OperatorID]bool)
	}
	i.valuePool[layer][vRoot][op] = true
}

// addToNoValuePool adds `op` to noValuePool[layer]. Idempotent.
func (i *Instance) addToNoValuePool(layer int, op OperatorID) {
	if i.noValuePool[layer] == nil {
		i.noValuePool[layer] = make(map[OperatorID]bool)
	}
	i.noValuePool[layer][op] = true
}

// removeFromNoValuePool removes `op` from noValuePool[layer]. Used by
// the upgrade-detection path (per §Pool aggregation rules / Receiver-
// side robustness): when ObserveValueMsg sees that the same op had
// previously emitted KindNoValue, the KindNoValue's noValuePool
// contribution is replaced by the upgrade KindValue's valuePool
// contribution.
func (i *Instance) removeFromNoValuePool(layer int, op OperatorID) {
	if i.noValuePool[layer] == nil {
		return
	}
	delete(i.noValuePool[layer], op)
}

// addToSigmaPool records the σ partial from op at (layer, vRoot).
// Idempotent on (layer, vRoot, op); the cached partial is overwritten if
// a different one is observed (defensive — Pigeonhole 2 + EKM make this
// not happen in honest paths).
func (i *Instance) addToSigmaPool(layer int, vRoot [32]byte, op OperatorID, partial Signature) {
	if i.sigmaPool[layer] == nil {
		i.sigmaPool[layer] = make(map[[32]byte]map[OperatorID]Signature)
	}
	if i.sigmaPool[layer][vRoot] == nil {
		i.sigmaPool[layer][vRoot] = make(map[OperatorID]Signature)
	}
	i.sigmaPool[layer][vRoot][op] = append(Signature{}, partial...)
}

// addToNrTagPool records the nr_tag_layer partial from op at this layer.
// Idempotent on (layer, op).
func (i *Instance) addToNrTagPool(layer int, op OperatorID, partial Signature) {
	if i.nrTagPool[layer] == nil {
		i.nrTagPool[layer] = make(map[OperatorID]Signature)
	}
	i.nrTagPool[layer][op] = append(Signature{}, partial...)
}

// valuePoolSize returns the number of distinct operators in
// valuePool[layer][vRoot].
func (i *Instance) valuePoolSize(layer int, vRoot [32]byte) int {
	if i.valuePool[layer] == nil {
		return 0
	}
	return len(i.valuePool[layer][vRoot])
}

// noValuePoolSize returns the number of distinct operators in
// noValuePool[layer].
func (i *Instance) noValuePoolSize(layer int) int {
	return len(i.noValuePool[layer])
}
