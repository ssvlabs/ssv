package base

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
)

// MaxCommitHashesPerOp caps the number of distinct KindCommit content
// hashes retained per operator at a single instance. The first distinct
// second hash fires Rule 3 cross-onion evidence (spec §Phase 2 single-emit
// rule); subsequent distinct hashes still fire one evidence each up to this
// cap, after which further distinct emissions are silently dropped to bound
// memory under abuse — the operator is already flagged byzantine many times
// over by then.
//
// Observability note: cap-reached drops are intentionally silent — no error
// returned (the API contract is "nil = applied or no-op"), no observer fire
// (the EvidenceObserver model is for byzantine attribution, not housekeeping).
// Operators wanting to see this boundary can infer it from the EvidenceObserver
// stream: ≥ MaxCommitHashesPerOp-1 Rule-3 cross-onion fires against the same
// (op, slot) implies further variants are being silently dropped.
const MaxCommitHashesPerOp = 8

// MaxRetainedPerOpLayer caps the number of distinct entries retained per
// (operator, layer) for inputs subject to retention bounds:
//
//   - Phase-1 bundles: bundles[layer][leader_id] — up to 2 distinct V's per
//     (layer, leader). The first distinct second V fires Rule 2 leader
//     equivocation evidence (spec §Phase 1 / Retention bounds).
//   - σ-side commit onion: peerOnions[layer][op] — up to 2 distinct
//     EncryptedLayer entries per (op, layer). The first distinct second
//     fires Rule 3 cross-onion equivocation (spec §Slashing-evidence).
//   - Wire-format witnesses: MaxWitnesses = MaxLayers × MaxRetainedPerOpLayer.
//
// Two is sufficient slashable evidence under f=1 byzantine assumption (one
// witnessed pair pins the byzantine); further accumulation is pure memory
// pressure with no attribution gain.
//
// Sibling-package note: `protocol/v2/obft/twoab` has the same constant
// name and value (`MaxRetainedPerOpLayer = 2`) but applies it only to
// Phase-1 bundle retention — twoab's σ partial lives in
// `KindValue.L0Partial`, so there's no separate σ-onion
// retention to bound. See docs/OBFT-TWOAB-CONVERGENCE-PLAN.md §L5.
const MaxRetainedPerOpLayer = 2

// CommitState is one operator's per-layer commitment state in the three-state
// model from spec §Phase 1 / Operator commitments. The σ / NR / NV states are
// local-per-layer; on the wire they materialize in a single KindCommit message
// per (operator, slot) emitted at T_commit, carrying σ partials for σ-state
// layers and NR partials for NR-state layers.
//
// NV (host-not-valid) is operationally identical to NR (silent-leader) —
// both materialize as an IBE partial on the layer's nr_tag. Local state
// distinguishes them only for telemetry / diagnostics.
type CommitState int

const (
	// CommitUndecided — initial state, before T_commit. The operator has
	// not yet committed at this layer.
	CommitUndecided CommitState = iota

	// CommitSigma — σ-emitted at T_commit on a single retained V whose host
	// validity check passed. EKM enforces single-σ-V per (slot, layer) and
	// σ-XOR-NR per layer; once committed, the operator may not emit NR/NV
	// nor σ on a different V.
	CommitSigma

	// CommitNRSilent — NR at T_commit. Either no V was retained at this
	// layer (silent-leader rule), or ≥ 2 distinct V's were retained
	// (equivocation rule, no winner-picking under f=1 byzantine).
	CommitNRSilent

	// CommitNV — NR at T_commit, host application returned `not-valid` for
	// the single retained V. Operationally identical to NR-silent on the
	// wire (both emit an IBE partial on nr_tag_k).
	CommitNV
)

func (s CommitState) String() string {
	switch s {
	case CommitUndecided:
		return "undecided"
	case CommitSigma:
		return "σ"
	case CommitNRSilent:
		return "NR-silent"
	case CommitNV:
		return "NV"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// witnessedSigma holds a leader's σ_V partial harvested from a peer's
// KindCommit witness section, paired with the V it was verified against.
// The signature bytes are deterministic BLS so all valid witnesses on the
// same V are byte-identical; the receiver stores one and treats it as the
// leader's σ-pool contribution. See Instance.witnessedLeaderSigma.
type witnessedSigma struct {
	Value Value
	Sigma Signature
}

// Instance is the per-slot OBFT state machine. It accumulates observations
// across Phase 1 (Phase-1 bundles), Phase 2 (peer Commits), and Phase 3
// (Resolve walk → final certificate gossip).
//
// Lifecycle (driven by the SSV adapter / Scheduler):
//
//  1. NewInstance(cfg, ...)
//  2. Phase 1 — for each layer:
//     a. If local op is the leader: BuildPhase1Bundle(layer, V) → bundle
//     for broadcast.
//     b. ObservePhase1Bundle(b, observedOffset) for each bundle received
//     from peers (or from the local op's own broadcast). Bundles
//     first-observed past T_commit at this operator are not retained.
//     c. ApplyHostValidity(layer, V, valid) once the host returns its
//     per-V validity verdict.
//  3. Phase 2 — at TCommit (single emission):
//     a. BuildOwnCommit() — emit a single KindCommit message carrying σ
//     partials for σ-state layers and NR partials for NR-state layers,
//     based on what was observed by T_commit.
//     b. ObserveCommit(c) for peers' KindCommit messages.
//  4. Phase 3 — from TCommit + Delta2 onward (no hard upper bound here;
//     the runner enforces the slot's relay-submission deadline out-of-band
//     via its scheduling layer):
//     a. Resolve() → Output (success) or ErrNoQuorum. Resolve is
//     stateless / idempotent — re-running on late KindCommit arrivals
//     can push σ-pool past qV at a layer that didn't reach on the
//     initial walk, or push NR-pool past qEnc to unlock the next
//     layer's chained decryption (Pigeonhole semantics still hold).
//     RoundEndOffset (= TCommit + Delta2 + Eps3) is a soft per-
//     operator target, not a hard cluster-wide deadline.
//     b. On success: BuildCertificate(out) → broadcast.
//     c. ObserveCertificate(c) for peers' certificates as a fallback
//     submission path when local Resolve fails.
//
// Instance is NOT thread-safe; callers must serialize access. The expected
// SSV adapter runs each Instance behind its own mutex (see
// protocol/v2/ssv/runner/obft.RunningInstance).
type Instance struct {
	cfg           *Config
	ownOperatorID OperatorID

	signer          obft.Signer
	tagSigner       obft.Signer
	ibe             obft.ThresholdIBE
	clusterPubKey   []byte
	pubKeyShares    map[OperatorID][]byte
	ibePubKeyShares map[OperatorID][]byte // optional under Option A

	// bundles[layer][leader_id] = retained Phase-1 bundles, capped at 2
	// distinct value_roots per spec §Phase 1 / Retention bounds.
	bundles map[int]map[OperatorID][]*Phase1Bundle

	// hostVerdict[layer][value_root] = host's valid/not-valid.
	// One entry per (layer, V); may be absent if host hasn't been asked.
	hostVerdict map[int]map[[32]byte]bool

	// host is the channel + dedup plumbing for peer-reflood-V host-validation
	// requests (see requestHostValidation). The runner drains its channel and
	// calls back via ApplyHostValidity with the verdict (driving l0DecisionReady
	// on the σ-via-peer-V branch). Closed by Finalize.
	host *obft.HostValidationGate

	// l0ReadyCh is closed when the operator has enough information at L_0 to
	// commit early (per spec §Phase 2 emission-timing): a uniquely-retained
	// host-validated V, OR a host not-valid verdict on the unique retained V,
	// OR leader equivocation observed (≥ 2 distinct V's). Closed at most once
	// per slot. Callers select on this against the T_commit fallback ticker.
	l0ReadyCh chan struct{}

	// peerOnions[layer][operator_id] = the σ-side onion entry seen from
	// this operator at this layer (extracted from their KindCommit). A
	// second distinct entry from the same (operator, layer) is cross-onion
	// equivocation evidence (Rule 3); since each operator emits exactly one
	// KindCommit per slot, the only way to observe two distinct entries is
	// a byzantine operator broadcasting two KindCommit messages.
	peerOnions map[int]map[OperatorID][]EncryptedLayer

	// peerNR[layer][operator_id] = the operator's NR partial for this layer
	// (extracted from their KindCommit's NRPartials).
	peerNR map[int]map[OperatorID]Signature

	// witnessedLeaderSigma[layer][value_root] = a leader's σ_V partial
	// harvested from a peer's KindCommit witness section, verified against
	// the leader's pubshare on the V whose sha256 matches value_root. Per
	// spec §Phase 2 wire format, witnesses are plaintext at every layer
	// (byte-for-byte copies of the leader's Phase-1 partial); receivers
	// who DID receive V from any source — Phase-1 bundle or any peer's
	// σ-onion entry carrying V — can use a peer's witness to recover σ_L^V
	// even if the leader's bundle σ_V never reached them directly.
	//
	// First-wins dedup: all honest peers' witnesses on the same V are
	// byte-identical (deterministic BLS partial), so the first valid one
	// short-circuits subsequent harvest attempts.
	//
	// Bucket size bound: not capped at MaxRetainedPerOpLayer at the
	// receiver, because under asymmetric retention (leader equivocates V_a,
	// V_b, V_c; some peers retain (V_a, V_b), others (V_a, V_c), etc.) a
	// single receiver may legitimately resolve > 2 distinct V's via
	// findVByRoot across all peers' σ-onion entries. Per peer's wire-level
	// cap (MaxWitnesses = MaxLayers × MaxRetainedPerOpLayer) and per-(slot,op)
	// admission cap in message/validation, the bucket size is bounded by
	// the leader's actual equivocation count — itself bounded by f-byzantine
	// at the leader role plus honest peers' bundle retention caps. In
	// practice the bucket holds 1 entry (no equivocation) or 2 entries
	// (single-leader Rule 2 equivocation).
	//
	// Limitation (v1): witnesses are only verified at observation time
	// against V that is already known (bundles[layer][leader].Value or any
	// peerOnions[layer][*].Value matching the witness's value_root). A
	// witness whose V isn't yet observed locally is dropped — no retroactive
	// re-verification once V later arrives. In the common ordering (Phase-1
	// bundle arrives at T_broadcast_max_k, KindCommits at T_commit), V is
	// known before witnesses; the cross-commit-out-of-order case (witness in
	// commit C1, V in commit C2 arriving after) is the residual coverage gap.
	witnessedLeaderSigma map[int]map[[32]byte]witnessedSigma

	// peerCommitHashes[operator_id] is the set of content hashes observed
	// from this operator. Identical re-broadcasts are no-ops; the first
	// distinct second hash is cross-onion equivocation evidence (spec §Phase 2,
	// single-emit rule). We retain all distinct hashes so further redeliveries
	// of either variant don't re-record evidence.
	peerCommitHashes map[OperatorID]map[[32]byte]struct{}

	// peerFirstCommit retains a deep copy of the FIRST KindCommit observed
	// from each operator, so the second distinct emission can fire top-level
	// Rule 3 evidence (CommitEquivocationEvidence) carrying both Commit
	// bodies as slashable proof. The entry is cleared after firing —
	// subsequent distinct emissions don't add slashable info (the operator
	// is already attributed) and would otherwise leak memory under abuse.
	peerFirstCommit map[OperatorID]*Commit

	// Local per-layer state.
	localState   []CommitState
	sigmaLocked  []bool
	sigmaLockedV []Value // when sigmaLocked[k], sigmaLockedV[k] is the V signed
	nrLocked     []bool

	// Own σ partials cached per layer (one per layer where this operator
	// is σ-state at T_commit). Single emission per slot in BuildOwnCommit.
	ownPartials map[int]Signature

	// True after BuildOwnCommit has emitted the operator's KindCommit
	// message. Used to enforce single-emission semantics.
	committed bool

	// receivedCertificate, if set, is a peer's final certificate that the
	// runner may use as an alternative submission path.
	receivedCertificate *Certificate

	// Evidence accumulator (slashing-evidence rules 1–5).
	evidence []Evidence

	// rule4Fired[layer][op] = true once Rule 4 (FakeEncryptedPresence) has
	// been recorded for that (operator, layer). A byzantine peer that emits
	// multiple distinct onion entries at a single layer (Rule 3 cross-onion
	// equivocation) would otherwise produce a Rule 4 entry per onion entry
	// at decrypt time. Deduplicated to one Rule 4 per (op, layer); the
	// underlying byzantine emission is already attributed via Rule 3.
	rule4Fired map[int]map[OperatorID]bool

	// rule1Fired[layer][op] = true once Rule 1 (CrossSigning) has been
	// recorded for that (operator, layer). Rule 1 has two fire sites — the
	// σ-side ObserveCommit branch fires when a σ-onion entry is added and an
	// NR partial already exists; the NR-side branch fires symmetrically.
	// Under Rule 3 equivocation (two distinct KindCommits from the same op),
	// the σ-side branch can fire on the second commit's σ-entry while the NR
	// dedup makes the NR-side a no-op — the result is one Rule 1 entry per
	// distinct σ-emission, not per (op, layer). Deduplication ensures
	// per-(op, layer) attribution stays atomic.
	rule1Fired map[int]map[OperatorID]bool

	// rule3LeaderFired[layer][leader] = true once the Rule 3 leader-cross-V
	// check has fired for that (leader, layer). Shared between the
	// forward-order path (ObserveCommit's leader-vs-bundle σ check) and
	// the retroactive path (reevaluateL0Sigmas when a bundle is retained
	// after the leader's L_0 onion was already in peerOnions) so the
	// dedup spans both arrival orderings: under leader equivocation +
	// onion-before-bundle ordering, the same fault would otherwise fire
	// once from the forward path on the first bundle retention and again
	// from the retro path on a second equivocated bundle.
	//
	// Distinct from the σ-side cross-onion equivocation path (two distinct
	// onion entries at the same layer from the same op) — that path has
	// its own structural dedup via len(existing) gates.
	rule3LeaderFired map[int]map[OperatorID]bool

	// rule5UnknownVFired[layer][op] = true once Rule 5 has fired against op
	// at this layer for the unknownV variant (op signed σ on a V the
	// receiver hasn't retained). Dedups across the immediate fire path
	// (ObserveCommit) and the retroactive path (reevaluateL0Sigmas, when a
	// bundle retention surfaces additional unknownV entries). cryptoFake
	// (partial fails verify against op's own share) retains its existing
	// multi-fire-per-entry behavior — it's unambiguous and the historical
	// per-(op, layer, entry) granularity is preserved for code-touch
	// minimization.
	rule5UnknownVFired map[int]map[OperatorID]bool

	// rule2Fired[layer][leader] = true once Rule 2 (LeaderEquivocation) has
	// been recorded for that (leader, layer). Shared between the bundle-vs-
	// bundle path (ObservePhase1Bundle, two distinct retained Phase-1 bundles
	// from the same leader) and the witness path (harvestWitness, two
	// distinct σ_V partials harvested for the same leader from peers'
	// commit witness sections). Dedup prevents both paths from firing for
	// the same logical fault when the leader's equivocation manifests both
	// as direct bundle pairs and indirectly via witness-derived σ's.
	rule2Fired map[int]map[OperatorID]bool

	// evidenceObserver fires on FIRST recording per (Rule, OperatorID, Layer)
	// tuple. Set at NewInstance construction; nil disables. Immutable post-
	// construction so there's no concurrency contract for callers to honor.
	//
	// Per spec §Slashing evidence, honest operators MUST log observed evidence
	// per-rule for out-of-band aggregation; this callback is the logging
	// surface (the SSV runner wires it to its preferred logger). The spec has
	// no dedicated on-wire evidence-gossip channel — underlying signed
	// messages (bundles, commits) propagate via normal protocol flow, and
	// cluster-wide attribution is recovered out-of-band by aggregating
	// operator logs (manual-blacklist mechanism is the canonical consumer).
	evidenceObserver EvidenceObserver
	evidenceObserved map[evidenceObservedKey]bool

	// ended is set by Finalize when the slot's instance is being torn down.
	// State-mutating methods (ObserveCommit, ObservePhase1Bundle,
	// ApplyHostValidity, BuildPhase1Bundle, BuildOwnCommit, ObserveCertificate,
	// Resolve) refuse to apply changes on a finalized instance, returning
	// ErrInstanceEnded. Without the check, a network goroutine that captured
	// the instance pointer before EndInstance ran could mutate state on an
	// officially-ended slot — e.g. add a late cryptoFake Rule 5 candidate
	// after operators have already consumed Evidence() for slashing handoff.
	//
	// The Controller adapter (RunningInstance) is the authoritative
	// serialization layer and additionally rejects mutator dispatches under
	// its instanceMu so the check fires before the Instance call even begins;
	// the per-mutator check below is defense-in-depth for any caller that
	// bypasses the adapter (direct Instance use in tests / future plumbing).
	ended bool

	// lastResolveTrace records the most recent Resolve walk's per-layer
	// state. Overwritten on every Resolve call (the walk is idempotent /
	// re-runnable, and later runs reflect more observed partials). Read
	// post-Resolve via LastResolveLayerAttempts() — consumed by the
	// consensustest framework's bucket-3 walk-consistency check.
	//
	// Cost: bounded by K (≤ N at the production K=N convention), one
	// struct append per layer per Resolve call. Negligible against the
	// crypto cost of σ-pool reconstruction at each layer.
	lastResolveTrace []LayerAttempt
}

// NewInstance creates a new OBFT instance bound to `ownOperatorID`.
//
// `signer` is the operator's V-keypair share signer (used for σ partials).
// `tagSigner` is the operator's IBE-keypair share signer (used for NR
// partials and aggregating into chained-decryption keys); if nil, falls
// back to `signer` — sufficient when the IBE primitive accepts the value-
// signer's aggregate format (Option A / DST trick).
//
// `clusterPubKey` is the IBE trust anchor (under Option A: validator's BLS
// pubkey; under Option B: cluster's IBE pubkey).
//
// `pubKeyShares` maps each operator's ID to their V-keypair pubkey share.
// `ibePubKeyShares` is the same for the IBE keypair; may be nil under
// Option A.
//
// `evidenceObserver`, if non-nil, fires once per (Rule, OperatorID, Layer)
// tuple on first observation. Set at construction so it's immutable post-
// construction — no concurrency contract for callers to honor (the alternative,
// SetEvidenceObserver post-construction, was a footgun: a writer racing with
// observation paths could publish a stale observer that misses early evidence).
// Pass nil to disable.
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
		return nil, errors.New("obft: nil signer or ibe")
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("obft: invalid config: %w", err)
	}
	if pubKeyShares == nil {
		return nil, errors.New("obft: nil pubKeyShares (need at least an empty map)")
	}
	// clusterPubKey is the IBE trust anchor used by chainEncryptForLayer
	// (Phase-2 σ-onion wrap at k > 0) and by ObserveCertificate's aggregate
	// verify. A nil/empty value would silently pass against permissive stub
	// signers in tests while a real BLS verifier would error opaquely deep in
	// the call stack — surface the misconfiguration up-front so it's caught
	// in test/setup paths.
	if len(clusterPubKey) == 0 {
		return nil, errors.New("obft: empty clusterPubKey (IBE trust anchor required)")
	}
	if !obft.OperatorInCluster(ownOperatorID, cfg.Operators) {
		return nil, fmt.Errorf("obft: own operator id %d not in cluster", ownOperatorID)
	}
	// Every operator in the cluster must have a registered pub-key share.
	// Layer leaders need it for Phase-3 reconstruction; non-leader peers
	// need it so peerSigmaAtL0Verdict / Verifier.VerifyPartial can run
	// against their share when their σ partials arrive. A missing share
	// would silently degrade Rule 5 detection (returning l0SigmaInconclusive)
	// and any later VerifyPartial against nil; surface the misconfiguration
	// upfront so it's caught in test/setup.
	for _, op := range cfg.Operators {
		if share, ok := pubKeyShares[op]; !ok || len(share) == 0 {
			return nil, fmt.Errorf("obft: operator %d has no pub-key share", op)
		}
	}
	if tagSigner == nil {
		tagSigner = signer
	}

	K := cfg.K()
	return &Instance{
		cfg:                  cfg,
		ownOperatorID:        ownOperatorID,
		signer:               signer,
		tagSigner:            tagSigner,
		ibe:                  ibe,
		clusterPubKey:        clusterPubKey,
		pubKeyShares:         pubKeyShares,
		ibePubKeyShares:      ibePubKeyShares,
		evidenceObserver:     evidenceObserver,
		bundles:              make(map[int]map[OperatorID][]*Phase1Bundle, K),
		hostVerdict:          make(map[int]map[[32]byte]bool, K),
		peerOnions:           make(map[int]map[OperatorID][]EncryptedLayer, K),
		peerNR:               make(map[int]map[OperatorID]Signature, K),
		peerCommitHashes:     make(map[OperatorID]map[[32]byte]struct{}),
		peerFirstCommit:      make(map[OperatorID]*Commit),
		localState:           make([]CommitState, K),
		sigmaLocked:          make([]bool, K),
		sigmaLockedV:         make([]Value, K),
		nrLocked:             make([]bool, K),
		ownPartials:          make(map[int]Signature),
		rule4Fired:           make(map[int]map[OperatorID]bool, K),
		rule1Fired:           make(map[int]map[OperatorID]bool, K),
		rule3LeaderFired:     make(map[int]map[OperatorID]bool, K),
		rule5UnknownVFired:   make(map[int]map[OperatorID]bool, K),
		rule2Fired:           make(map[int]map[OperatorID]bool, K),
		witnessedLeaderSigma: make(map[int]map[[32]byte]witnessedSigma, K),
		evidenceObserved:     make(map[evidenceObservedKey]bool),
		l0ReadyCh:            make(chan struct{}),
		host:                 obft.NewHostValidationGate(K),
	}, nil
}

// WantsHostValidationCh returns the channel on which the Instance delivers
// host-validity requests for V's first-observed via peer commits. The runner
// MUST drain this channel (typically via select alongside its other
// per-slot signals) and dispatch validation through the host hook,
// invoking ApplyHostValidity with the verdict.
//
// Buffered with capacity K; the Instance drops enqueue attempts if the
// buffer is full (back-pressure: a slow runner that isn't draining means
// the validation path is degraded, but the Instance never blocks). The
// channel is closed by Finalize.
func (i *Instance) WantsHostValidationCh() <-chan ValidationRequest {
	return i.host.Channel()
}

// requestHostValidation enqueues a host-validity request on the wants
// channel if (a) host hasn't already validated this V at this layer, and
// (b) no in-flight request exists for the same (layer, value_root).
// Non-blocking: drops on full buffer.
//
// Post-Finalize guard: sending on a closed channel panics even via
// `select` with default. Today base's mutator entry points all gate on
// `i.ended` before reaching this helper (the sole caller is
// maybeRequestL0PeerVValidation in phase2.go, called only from
// ObserveCommit which is `i.ended`-guarded), so this defense-in-depth
// guard protects against a future refactor that adds a new caller from
// an unguarded path. Mirrors the equivalent guard in twoab's
// requestHostValidation.
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

// L0ReadyCh returns a channel closed when the operator has enough information
// at L_0 to commit early per spec §Phase 2 emission-timing. Becomes ready when:
//   - the operator is the L_0 leader and has built their Phase-1 bundle
//     (sigmaLocked[0] is set on transitionToSigma during BuildPhase1Bundle), OR
//   - ≥ 2 distinct V's are observable at L_0 across (retained bundles ∪ peer
//     onions ∪ verified σ_L^V witnesses) → forced NR per cross-phase
//     exclusivity, OR
//   - a uniquely-retained Phase-1 bundle at L_0 exists AND host has returned a
//     valid/not-valid verdict on it (primary σ path), OR
//   - no retained bundle but a uniquely-observed peer-V at L_0 with verified
//     leader σ_L^V (witnessedLeaderSigma) AND host validity recorded
//     (peer-reflood-V σ path, see spec §Phase 2 / Peer-reflood V via early
//     commit).
//
// The channel is closed at most once per slot. If L_0 never becomes ready
// (silent leader or grossly-late bundle), the channel stays open and the
// caller's T_commit ticker fires the fallback emit.
func (i *Instance) L0ReadyCh() <-chan struct{} { return i.l0ReadyCh }

// l0DecisionReady reports whether L_0 has a stable σ/NR decision available
// (predicate underlying L0ReadyCh).
//
// Fires when any of:
//   - L_0 leader path: this operator is the leader and has built their
//     Phase-1 bundle (sigmaLocked[0] is set on transitionToSigma during
//     BuildPhase1Bundle).
//   - Equivocation observed: ≥ 2 distinct V's known at L_0 across any
//     source — retained Phase-1 bundles (bundles[0][leader]) OR
//     verified σ_L^V witnesses (witnessedLeaderSigma[0]). Forced NR per
//     cross-phase exclusivity.
//   - Primary σ path (Phase-1 retention): unique retained bundle at L_0
//     AND host validity verdict recorded (any verdict: valid → σ, NV → NR
//     equivalent; protocol commits the decision either way).
//   - Peer-reflood-V σ path (spec §Phase 2 / Peer-reflood V via early
//     commit): no retained bundle, but a uniquely-observed peer-V at L_0
//     with a verified leader σ_L^V (witnessedLeaderSigma[0][ValueRoot(V)])
//     AND host validity verdict recorded.
func (i *Instance) l0DecisionReady() bool {
	const layer = 0
	// L_0 leader path: BuildPhase1Bundle emits the leader's Phase-1 σ_V which
	// counts as their σ-side commitment at this layer (spec OBFT.md §Phase 2
	// "The layer-k leader's Phase-1 σ_V counts as their σ-side commitment").
	if i.sigmaLocked[layer] {
		return true
	}
	// Equivocation observed across (bundles ∪ peerOnions ∪
	// witnessedLeaderSigma): ≥ 2 distinct V's known at L_0 → forced NR
	// per cross-phase exclusivity.
	if i.distinctVCountAtLayer(layer) >= 2 {
		return true
	}
	// Primary σ path: uniquely-retained bundle + host verdict recorded.
	leaderMap := i.bundles[layer]
	expectedLeader := i.cfg.Layers[layer].Leader
	retained := leaderMap[expectedLeader]
	if len(retained) == 1 {
		verdicts := i.hostVerdict[layer]
		if verdicts == nil {
			return false
		}
		_, recorded := verdicts[ValueRoot(retained[0].Value)]
		return recorded
	}
	// Peer-reflood-V σ path: no bundle locally, but a uniquely-observed
	// peer-V at L_0 with verified leader σ_L^V + host verdict recorded.
	if v, ok := i.uniquePeerV(layer); ok {
		if _, witnessed := i.witnessedLeaderSigma[layer][ValueRoot(v)]; !witnessed {
			return false
		}
		verdicts := i.hostVerdict[layer]
		if verdicts == nil {
			return false
		}
		_, recorded := verdicts[ValueRoot(v)]
		return recorded
	}
	return false
}

// distinctVCountAtLayer counts the number of distinct V's known at `layer`
// across retained Phase-1 bundles, peer σ-onion entries, and verified
// leader σ_L^V witnesses. ≥ 2 → cluster-observable equivocation; per
// cross-phase exclusivity the operator must NR at this layer regardless
// of which source first exposed the second V.
//
// Used by l0DecisionReady (signal the NR-ready predicate) and by
// chosenVForLayer (gate σ-emission so the operator doesn't σ on V_a
// when V_b is also observable via peer-V/witness).
func (i *Instance) distinctVCountAtLayer(layer int) int {
	seen := make(map[[32]byte]bool)
	expectedLeader := i.cfg.Layers[layer].Leader
	for _, b := range i.bundles[layer][expectedLeader] {
		seen[ValueRoot(b.Value)] = true
	}
	for _, entries := range i.peerOnions[layer] {
		for _, el := range entries {
			if len(el.Value) == 0 {
				continue
			}
			seen[ValueRoot(el.Value)] = true
		}
	}
	for root := range i.witnessedLeaderSigma[layer] {
		seen[root] = true
	}
	return len(seen)
}

// maybeSignalL0Ready closes l0ReadyCh if the L_0 decision has just become
// available. Safe to call repeatedly; subsequent calls are no-ops.
func (i *Instance) maybeSignalL0Ready() {
	select {
	case <-i.l0ReadyCh:
		return
	default:
	}
	if i.l0DecisionReady() {
		close(i.l0ReadyCh)
	}
}

// Config returns the instance's config (read-only).
func (i *Instance) Config() *Config { return i.cfg }

// OwnOperatorID returns the local operator's ID.
func (i *Instance) OwnOperatorID() OperatorID { return i.ownOperatorID }

// LeaderAtLayers returns the layer indices where the local operator is the
// designated leader for this slot. Empty if not a leader at any layer.
func (i *Instance) LeaderAtLayers() []int {
	var layers []int
	for k, ls := range i.cfg.Layers {
		if ls.Leader == i.ownOperatorID {
			layers = append(layers, k)
		}
	}
	return layers
}

// LocalState returns this operator's commitment state at `layer`.
func (i *Instance) LocalState(layer int) CommitState {
	if layer < 0 || layer >= i.cfg.K() {
		return CommitUndecided
	}
	return i.localState[layer]
}

// InstanceStats is an introspection snapshot of internal counters that
// are otherwise unobservable through the public API. Stable across
// versions: fields are additive (new counters appended, never removed
// or renamed). Intended for tests, telemetry, and diagnostic tooling.
// Mirrors the equivalent type in twoab/instance.go (with package-
// specific field set).
type InstanceStats struct {
	// PendingValidationCount is the total number of (layer, V_root)
	// pairs in the pendingValidation set, summed across all layers.
	// Semantic note: in base, entries are inserted on request and only
	// removed on the buffer-full rollback path — there is no removal
	// on host-reply arrival, so this counter is monotonic-after-
	// rollback rather than "currently outstanding". The counter
	// reflects "requests issued that didn't immediately drop on
	// buffer-full". For an "active request set" semantic, twoab's
	// equivalent removes on ApplyHostValidity; base does not.
	PendingValidationCount int

	// WitnessedLeaderSigmaCount is the total number of (layer, V_root)
	// entries cached in witnessedLeaderSigma — successful leader σ_V
	// partial verifications harvested from peers' KindCommit.Witnesses[]
	// (the explicit witness section of the commit wire format, NOT the
	// σ-onion entries in peerOnions). Summed across all layers.
	WitnessedLeaderSigmaCount int

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
// per-method API doesn't surface (e.g., the pendingValidation set's
// size, the witnessed-leader-σ cache).
func (i *Instance) Stats() InstanceStats {
	pending := i.host.PendingCount()
	witnessed := 0
	for _, bucket := range i.witnessedLeaderSigma {
		witnessed += len(bucket)
	}
	return InstanceStats{
		PendingValidationCount:    pending,
		WitnessedLeaderSigmaCount: witnessed,
		EvidenceCount:             len(i.evidence),
		Ended:                     i.ended,
	}
}

// Evidence returns the accumulated slashing-evidence entries.
//
// The returned slice is a snapshot copy: appending to it or reordering its
// elements does not affect internal state. However, each Evidence struct
// carries a per-rule pointer (CrossSigning, LeaderEquivocation, ...) whose
// payload is *shared* with internal state — mutating, e.g.,
// `out[i].CrossSigning.SigmaPartial[0] = 0` would corrupt the Instance's
// own copy. Callers MUST treat the returned entries as read-only.
//
// Deep-copying per-rule payloads on every call would be wasteful (slashing
// handoff typically reads Evidence once at end-of-slot); the read-only
// contract is the right trade-off.
func (i *Instance) Evidence() []Evidence {
	out := make([]Evidence, len(i.evidence))
	copy(out, i.evidence)
	return out
}

// Finalize seals the Instance: subsequent mutator calls (ObserveCommit etc.)
// refuse to apply state changes. The `ended` flag is checked after acquiring
// the instance mutex by every caller so a network goroutine that captured
// the instance pointer pre-finalize is fenced.
//
// Idempotent — safe to call repeatedly.
//
// Note on Rule 5 attribution scope: both the cryptoFake verdict (partial
// fails verify against op's own pubshare on the claimed V — unambiguous
// byzantine) and the unknownV verdict (partial verifies but claimed V is
// not retained locally) fire Rule 5 per spec §Slashing evidence. The
// unknownV path is gated to non-leaders (leader cross-V is Rule 3) and is
// per-receiver-view by construction: under leader equivocation an honest
// peer who signed an equivocated V the receiver hasn't observed is logged
// as Rule 5 here. Per spec MUST-log framing, single-receiver evidence is
// expected to be reconciled by the out-of-band aggregation layer (the
// manual-blacklist mechanism is the canonical consumer); false-positives
// from honest equivocation reactions are visible as evidence from a
// receiver minority while the equivocated-V-retaining majority logs no
// Rule 5 (or Rule 3 against the leader instead).
func (i *Instance) Finalize() {
	if i.ended {
		return
	}
	i.ended = true
	// Close the host-validation request channel so any runner blocking on
	// a select case for it observes the close and unblocks. Safe to call
	// once (ended-flag dedup above).
	i.host.Close()
}

// Ended reports whether this Instance has been Finalize'd. Caller must hold
// the surrounding instance mutex (the Controller's RunningInstance.instanceMu
// in production). State-mutating methods consult this to refuse late writes.
func (i *Instance) Ended() bool {
	return i.ended
}

// RetainedBundles returns the bundles retained for (layer, leader). Up to 2
// distinct bundles per spec retention bound. Useful for slashing-evidence
// packaging by callers.
func (i *Instance) RetainedBundles(layer int, leader OperatorID) []*Phase1Bundle {
	leaderMap, ok := i.bundles[layer]
	if !ok {
		return nil
	}
	src := leaderMap[leader]
	out := make([]*Phase1Bundle, len(src))
	copy(out, src)
	return out
}

// Helpers

// chainEncryptForLayer encrypts `partial` for layer `k` using the chained-IBE
// construction from spec §Phase 2:
//
//	layer k:  E_{nr_tag_0}( ... E_{nr_tag_{k-1}}( σ_partial ) ... )
//
// The innermost wrap uses nr_tag_{k-1}; the outermost uses nr_tag_0.
func (i *Instance) chainEncryptForLayer(k int, partial []byte) ([]byte, error) {
	return obft.ChainEncryptForLayer(i.ibe, i.clusterPubKey, i.cfg.ClusterID, i.cfg.Height, k, partial)
}

// chainDecryptForLayer decrypts a layer-k onion entry using `decryptionKeys`
// where decryptionKeys[j] is the aggregated NR-partials sig on nr_tag_j.
//
// Decryption applies keys outermost-first:
//
//	D_{nr_tag_0}( D_{nr_tag_1}( ... D_{nr_tag_{k-1}}( ciphertext ) ... ) )
//
// Returns the recovered σ partial bytes.
func (i *Instance) chainDecryptForLayer(k int, ciphertext []byte, decryptionKeys [][]byte) ([]byte, error) {
	return obft.ChainDecryptForLayer(i.ibe, k, ciphertext, decryptionKeys)
}

// transitionToSigma applies the σ-emit EKM lock for `layer` on `value`.
// Returns ErrSigmaLocked if already locked on a different V; ErrNRLocked if
// the operator already NR-committed at this layer.
//
// On success, sigmaLocked[layer] = true, sigmaLockedV[layer] = value, and
// localState[layer] = CommitSigma.
func (i *Instance) transitionToSigma(layer int, value Value) error {
	if i.nrLocked[layer] {
		return ErrNRLocked
	}
	if i.sigmaLocked[layer] {
		// Idempotent if same V; reject otherwise (single-σ-V invariant).
		if !bytes.Equal(i.sigmaLockedV[layer], value) {
			return ErrSigmaLocked
		}
		return nil
	}
	i.sigmaLocked[layer] = true
	i.sigmaLockedV[layer] = append(Value{}, value...)
	i.localState[layer] = CommitSigma
	return nil
}

// transitionToNR applies the NR-emit EKM lock for `layer`. Returns
// ErrSigmaLocked if the operator already σ-committed at this layer.
//
// `state` distinguishes NR-silent from NV (operationally identical, local
// diagnostic only).
func (i *Instance) transitionToNR(layer int, state CommitState) error {
	if state != CommitNRSilent && state != CommitNV {
		return fmt.Errorf("obft: invalid NR target state %s", state)
	}
	if i.sigmaLocked[layer] {
		return ErrSigmaLocked
	}
	if i.nrLocked[layer] {
		// Idempotent.
		return nil
	}
	i.nrLocked[layer] = true
	i.localState[layer] = state
	return nil
}

// recordEvidence appends a non-nil evidence entry to the accumulator and
// fires the (rate-limited) EvidenceObserver on first observation.
func (i *Instance) recordEvidence(e Evidence) {
	i.evidence = append(i.evidence, e)

	// First-observation observer fire. Per-(Rule, Op, Layer) dedup ensures
	// the observer (typically a logger) sees one entry per distinct
	// attributable fault — repeated detections of the same fault (e.g.,
	// Rule 3 redundant emissions) do not re-fire.
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

// recordRulePerLayer is the shared template for per-(op, layer) evidence
// rules — all of base's slashing rules are keyed by (layer × op).
// Lazily initializes the inner per-op map at `table[layer]`, then marks
// (op, layer) as fired. Returns true on first observation, false if the
// rule had already been recorded for this (op, layer) pair.
//
// Maps are reference types in Go — passing `table` by value still
// allows the helper to populate `table[layer] = make(...)` and have
// the mutation observed by the caller (the map header is a pointer-
// like value sharing the backing hash table). Mirrors the equivalent
// helper in twoab/instance.go.
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

// recordRule4 marks Rule 4 (FakeEncryptedPresence) as fired for (op, layer).
// Returns true if this is the first observation (caller should record evidence)
// and false if Rule 4 was already fired for this pair (caller should skip).
func (i *Instance) recordRule4(op OperatorID, layer int) bool {
	return i.recordRulePerLayer(i.rule4Fired, op, layer)
}

// recordRule1 marks Rule 1 (CrossSigning) as fired for (op, layer).
func (i *Instance) recordRule1(op OperatorID, layer int) bool {
	return i.recordRulePerLayer(i.rule1Fired, op, layer)
}

// recordRule3Leader marks the Rule 3 leader-cross-V check as fired for
// (leader, layer). Shared between ObserveCommit's forward-order
// leader-vs-bundle check and reevaluateL0Sigmas' retroactive check; the
// first one to fire wins, the other becomes a no-op. See the
// rule3LeaderFired field comment.
func (i *Instance) recordRule3Leader(op OperatorID, layer int) bool {
	return i.recordRulePerLayer(i.rule3LeaderFired, op, layer)
}

// recordRule5UnknownV marks Rule 5 as fired against (op, layer) for the
// unknownV variant. See rule5UnknownVFired field comment.
func (i *Instance) recordRule5UnknownV(op OperatorID, layer int) bool {
	return i.recordRulePerLayer(i.rule5UnknownVFired, op, layer)
}

// recordRule2 marks Rule 2 (LeaderEquivocation) as fired for (leader, layer).
// See rule2Fired field comment.
func (i *Instance) recordRule2(leader OperatorID, layer int) bool {
	return i.recordRulePerLayer(i.rule2Fired, leader, layer)
}

// findExistingLeaderSigmaOnDistinctV scans bundles[layer][leader] and
// witnessedLeaderSigma[layer] for an entry whose Value differs from newV.
// Returns a Phase1Bundle representing that existing entry: a real retained
// bundle if available, otherwise a synthesized one from the witnessed σ
// (carrying the witnessed V + σ_V, with this Instance's ClusterID/Height/
// Layer/Leader filling the wire-shape fields).
//
// Used by both ObservePhase1Bundle and harvestWitness to detect Rule 2
// (leader equivocation) across all source combinations: bundle vs bundle,
// bundle vs witness, witness vs witness. Synthesized bundles are tagged
// for slashing-layer consumption: their authenticity stands on the
// receiver's local verification of σ_V (against the leader's pubshare)
// plus the witnessing peer's SSV-signed Commit, rather than the leader's
// own direct Phase1Bundle envelope. Out-of-band aggregation pairs synth
// evidence with the corresponding peer Commit envelopes.
func (i *Instance) findExistingLeaderSigmaOnDistinctV(layer int, leader OperatorID, newV Value) *Phase1Bundle {
	for _, b := range i.bundles[layer][leader] {
		if !bytes.Equal(b.Value, newV) {
			return b
		}
	}
	for _, ws := range i.witnessedLeaderSigma[layer] {
		if !bytes.Equal(ws.Value, newV) {
			return &Phase1Bundle{
				ClusterID:   i.cfg.ClusterID,
				OperatorID:  leader,
				Height:      i.cfg.Height,
				Layer:       layer,
				Value:       append(Value{}, ws.Value...),
				LeaderSigma: append(Signature{}, ws.Sigma...),
			}
		}
	}
	return nil
}

// observedTimeOK reports whether `observedOffset` is within the receiver
// acceptance window for Phase-1 bundles ([slot_start, T_commit]).
//
// Per spec, bundles first-observed past T_commit at any honest receiver are
// not counted by that receiver toward σ-quorum at this layer; the cluster
// relies on K-layer fall-through for partition recovery (no Defer state).
func (i *Instance) observedTimeOK(observedOffset time.Duration) bool {
	return observedOffset >= 0 && observedOffset <= i.cfg.PhaseTwoStartOffset()
}
