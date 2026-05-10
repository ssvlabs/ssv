package obft

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"
)

// MaxCommitHashesPerOp caps the number of distinct KindCommit content
// hashes retained per operator at a single instance. The first distinct
// second hash fires Rule 3 cross-onion evidence (spec §Phase 2 single-emit
// rule); subsequent distinct hashes still fire one evidence each up to this
// cap, after which further distinct emissions are silently dropped to bound
// memory under abuse — the operator is already flagged byzantine many times
// over by then.
const MaxCommitHashesPerOp = 8

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
//     RoundEndOffset (= TCommit + Delta2 + Delta3) is a soft per-
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

	signer          Signer
	tagSigner       Signer
	ibe             ThresholdIBE
	clusterPubKey   []byte
	pubKeyShares    map[OperatorID][]byte
	ibePubKeyShares map[OperatorID][]byte // optional under Option A

	// bundles[layer][leader_id] = retained Phase-1 bundles, capped at 2
	// distinct value_roots per spec §Phase 1 / Retention bounds.
	bundles map[int]map[OperatorID][]*Phase1Bundle

	// hostVerdict[layer][string(value_root)] = host's valid/not-valid.
	// One entry per (layer, V); may be absent if host hasn't been asked.
	hostVerdict map[int]map[string]bool

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

	// evidenceObserver fires on FIRST recording per (Rule, OperatorID, Layer)
	// tuple. Set at NewInstance construction; nil disables. Immutable post-
	// construction so there's no concurrency contract for callers to honor.
	//
	// Spec §Slashing evidence Rule 5 MUST-gossip rule says receivers MUST
	// gossip evidence on the wire so no-retained-V receivers can also
	// attribute. This impl substitutes out-of-band logging — the observer
	// surfaces evidence to the SSV runner, which logs it for operator
	// review. (Ideally this should be on-wire to spread evidence cluster-
	// wide automatically; logged-only is a deliberate scope choice —
	// operators monitor logs out-of-band.)
	evidenceObserver EvidenceObserver
	evidenceObserved map[evidenceObservedKey]bool

	// ended is set by Finalize when the slot's instance is being torn down.
	// Per-instance state-mutating methods (ObserveCommit, ObservePhase1Bundle,
	// ApplyHostValidity, ...) check this flag after acquiring the instance
	// mutex and refuse to mutate state on a finalized instance. Without the
	// check, a network goroutine that captured the instance pointer before
	// EndInstance ran could mutate state on an officially-ended slot — e.g.
	// add a late cryptoFake Rule 5 candidate after operators have already
	// consumed Evidence() for slashing handoff.
	ended bool
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
	signer Signer,
	tagSigner Signer,
	ibe ThresholdIBE,
	clusterPubKey []byte,
	pubKeyShares map[OperatorID][]byte,
	ibePubKeyShares map[OperatorID][]byte,
	evidenceObserver EvidenceObserver,
) (*Instance, error) {
	if cfg == nil || signer == nil || ibe == nil {
		return nil, errors.New("obft: nil config / signer / ibe")
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("obft: invalid config: %w", err)
	}
	if pubKeyShares == nil {
		return nil, errors.New("obft: nil pubKeyShares (need at least an empty map)")
	}
	if !operatorInCluster(ownOperatorID, cfg) {
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
		cfg:              cfg,
		ownOperatorID:    ownOperatorID,
		signer:           signer,
		tagSigner:        tagSigner,
		ibe:              ibe,
		clusterPubKey:    clusterPubKey,
		pubKeyShares:     pubKeyShares,
		ibePubKeyShares:  ibePubKeyShares,
		evidenceObserver: evidenceObserver,
		bundles:          make(map[int]map[OperatorID][]*Phase1Bundle, K),
		hostVerdict:      make(map[int]map[string]bool, K),
		peerOnions:       make(map[int]map[OperatorID][]EncryptedLayer, K),
		peerNR:           make(map[int]map[OperatorID]Signature, K),
		peerCommitHashes: make(map[OperatorID]map[[32]byte]struct{}),
		peerFirstCommit:  make(map[OperatorID]*Commit),
		localState:       make([]CommitState, K),
		sigmaLocked:      make([]bool, K),
		sigmaLockedV:     make([]Value, K),
		nrLocked:         make([]bool, K),
		ownPartials:      make(map[int]Signature),
		rule4Fired:       make(map[int]map[OperatorID]bool, K),
		rule1Fired:       make(map[int]map[OperatorID]bool, K),
	}, nil
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

// Evidence returns the accumulated slashing-evidence entries (snapshot copy).
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
// Note on Rule 5 deferred attribution: σ entries at L_0 that verify
// cryptographically against the operator's pubshare but sign a V the receiver
// never retained (l0SigmaUnknownV) are NOT fired as Rule 5 evidence here.
// Under leader equivocation + asymmetric gossipsub propagation, an honest
// peer who signed an equivocated V the receiver didn't get would be falsely
// attributed if we fired Rule 5 on l0SigmaUnknownV at slot end. Per spec
// §Slashing evidence Rule 5, the spec's mitigation is on-wire MUST-gossip:
// every retained-V receiver gossips Rule 5 evidence (rate-limited per
// (slot, layer, operator_id)) so cluster-wide attribution converges and
// honest receivers' "I have V" attestations distinguish "byzantine signed
// fake V" from "honest signed V I didn't get". The on-wire gossip is not
// yet implemented in this codebase — until it lands, we trade Rule 5
// attribution coverage for the unknownV case (false-negative-prone) for
// strict no-false-positives. The cryptoFake case (σ doesn't verify against
// op's own share for the claimed V) still fires Rule 5 immediately at
// observe time; that case is unambiguous regardless of gossipsub propagation.
func (i *Instance) Finalize() {
	if i.ended {
		return
	}
	i.ended = true
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

// valueRootKey returns a string key suitable for use in maps keyed by
// value_root (the SHA-256 hash of a value).
func valueRootKey(v Value) string {
	h := sha256.Sum256(v)
	return string(h[:])
}

// chainEncryptForLayer encrypts `partial` for layer `k` using the chained-IBE
// construction from spec §Phase 2:
//
//	layer k:  E_{nr_tag_0}( ... E_{nr_tag_{k-1}}( σ_partial ) ... )
//
// The innermost wrap uses nr_tag_{k-1}; the outermost uses nr_tag_0.
func (i *Instance) chainEncryptForLayer(k int, partial []byte) ([]byte, error) {
	if k == 0 {
		return partial, nil // L_0 plaintext
	}
	inner := partial
	// Wrap from innermost (nr_tag_{k-1}) to outermost (nr_tag_0).
	for j := k - 1; j >= 0; j-- {
		tag := NoQuorumTag(i.cfg.ClusterID, i.cfg.Height, j)
		ct, err := i.ibe.Encrypt(i.clusterPubKey, tag, inner)
		if err != nil {
			return nil, fmt.Errorf("encrypt at chain level %d: %w", j, err)
		}
		inner = ct
	}
	return inner, nil
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
	if k == 0 {
		return ciphertext, nil // L_0 plaintext
	}
	if len(decryptionKeys) < k {
		return nil, fmt.Errorf("obft: need %d chained-decryption keys for layer %d, have %d",
			k, k, len(decryptionKeys))
	}
	outer := ciphertext
	for j := 0; j < k; j++ {
		pt, err := i.ibe.Decrypt(outer, decryptionKeys[j])
		if err != nil {
			return nil, fmt.Errorf("decrypt at chain level %d: %w", j, err)
		}
		outer = pt
	}
	return outer, nil
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

// EvidenceObserver is invoked the FIRST time an Evidence with a given
// (Rule, OperatorID, Layer) tuple is recorded by an Instance. Subsequent
// records for the same tuple do NOT re-fire (e.g., a Rule 3 equivocation
// fires once per (operator, layer) regardless of how many redundant
// emissions trigger detection). Implementations should be non-blocking;
// the callback runs synchronously inside the protocol layer's recording
// path.
//
// Per spec §Slashing evidence Rule 5, the spec mandates MUST-gossip on
// the wire so no-retained-V receivers can also attribute. This impl
// substitutes out-of-band logging via this observer — see the
// evidenceObserver field comment on Instance.
type EvidenceObserver func(Evidence)

type evidenceObservedKey struct {
	rule  EvidenceRule
	op    OperatorID
	layer int
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
	if i.evidenceObserved == nil {
		i.evidenceObserved = make(map[evidenceObservedKey]bool)
	}
	key := evidenceObservedKey{rule: e.Rule, op: e.OperatorID, layer: e.Layer}
	if i.evidenceObserved[key] {
		return
	}
	i.evidenceObserved[key] = true
	i.evidenceObserver(e)
}

// recordRule4 marks Rule 4 (FakeEncryptedPresence) as fired for (op, layer).
// Returns true if this is the first observation (caller should record evidence)
// and false if Rule 4 was already fired for this pair (caller should skip).
func (i *Instance) recordRule4(op OperatorID, layer int) bool {
	bucket := i.rule4Fired[layer]
	if bucket == nil {
		bucket = make(map[OperatorID]bool)
		i.rule4Fired[layer] = bucket
	}
	if bucket[op] {
		return false
	}
	bucket[op] = true
	return true
}

// recordRule1 marks Rule 1 (CrossSigning) as fired for (op, layer). Same
// pattern as recordRule4 — returns true on first observation, false if
// already recorded.
func (i *Instance) recordRule1(op OperatorID, layer int) bool {
	bucket := i.rule1Fired[layer]
	if bucket == nil {
		bucket = make(map[OperatorID]bool)
		i.rule1Fired[layer] = bucket
	}
	if bucket[op] {
		return false
	}
	bucket[op] = true
	return true
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
