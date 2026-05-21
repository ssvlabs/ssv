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
//  1. Phase 1 — [slot_start, T_0_broadcast]:
//     a. If local operator is a layer leader: BuildPhase1Bundle(layer, V)
//     constructs a bundle and broadcasts it. NO σ_V partial (2abOBFT
//     emits σ at Phase 2b uniformly with all operators).
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
//     received V_0 + host valid emit an upgrade KindValue (A1 sequence).
//
//  4. Phase 2b — dynamic, no protocol-level deadline:
//     a. MaybeBuildAndBroadcastCommit() — each op evaluates the three
//     triggers (equivocation > σ-eligibility > NR-eligibility with cannot-σ
//     gate); fires at most one KindCommit per slot.
//     b. ObserveCommit(c) for peers (Side ∈ {Signed, NR, NRDirect}).
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

	// hostVerdict[layer][string(value_root)] = host application's
	// valid/not-valid verdict on the V identified by value_root at this
	// layer. Populated via ApplyHostValidity; consumed by
	// ComputeLocalValueState (at Phase-2a fire-time) and by the σ-eligibility
	// trigger's side decision (at Commit emission time, for host re-check).
	hostVerdict map[int]map[string]bool

	// Phase-2a own emissions. ownNoValueMsg is set if the op emitted
	// NoValue at Phase 2a; ownValueMsg is set if the op emitted Value at
	// Phase 2a OR as a Phase-2a-late A1 upgrade from a prior NoValue. Both
	// non-nil indicates the upgrade happened (A1 sequence). Phase-2a
	// NRDirect emitters set ownCommit directly (Side=NRDirect) and leave
	// ownValueMsg / ownNoValueMsg nil.
	ownValueMsg   *ValueMsg
	ownNoValueMsg *NoValueMsg

	// Phase-2b own emission. At most one Commit per slot per operator,
	// per spec §Phase 2b. Set by MaybeBuildAndBroadcastCommit OR by
	// MaybeFirePhase2a (when ComputeLocalValueState returns NRDirect).
	ownCommit *Commit

	// Peer Phase-2 emissions — first-observed retained per (slot, op).
	// Used for:
	//   - Inference (KindCommit-Signed implies KindValue existed per A2/A6).
	//   - Duplicate / equivocation detection (second distinct emission =
	//     Phase-2 equivocation per Rule 6a, unless authorized by A1/A2/.../A8).
	//   - Pool aggregation per §Pool aggregation rules.
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
	// Pool membership is per the inference rules: an op's KindValue at L_0
	// adds them to valuePool[0][ValueRoot]; their L_k>0 SigmaChained entry
	// adds them to valuePool[k][V_root]; their KindNoValue adds them to
	// noValuePool[0]; etc. KindCommit-Signed implies an earlier KindValue
	// (per A2/A6) and adds the op to valuePool[0][ValueRoot] via inference.
	valuePool   map[int]map[[32]byte]map[OperatorID]bool
	noValuePool map[int]map[OperatorID]bool

	// Threshold pools (the actual σ and NR partials used for Phase 3
	// reconstruction). Distinct from claim pools — these only contain
	// extractable partial signatures, not inferred-claim memberships.
	//
	//   - sigmaPool[layer][V_root][op] = the op's σ partial on V at this
	//     layer. At L_0: extracted from KindCommit-Signed. At L_k>0:
	//     decrypted from Phase-2a LayerEntry (SigmaChained, peeled via
	//     accumulated nr_tag keys).
	//   - nrTagPool[layer][op] = the op's nr_tag_k partial at this layer.
	//     At L_0: extracted from KindCommit-NR / KindCommit-NRDirect. At
	//     L_k>0: extracted from Phase-2a LayerEntry (NRPlaintext).
	//
	// Threshold pools are populated lazily as the Phase 3 walk needs them
	// (L_0 sigmaPool is populated on KindCommit-Signed observation; L_k>0
	// is populated during Resolve()'s chain-decryption walk once enough
	// nr_tag partials have aggregated to unlock the layer).
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

	// Per-rule dedup buckets — ensure one Evidence entry per logical
	// fault even when multiple detection paths fire.
	rule1Fired                 map[int]map[OperatorID]bool // Rule 1 cross-signing per (layer, op)
	rule3Fired                 map[int]map[OperatorID]bool // Rule 3 per (layer, op)
	rule4Fired                 map[int]map[OperatorID]bool // Rule 4 fake encrypted-presence per (layer, op)
	rule5Fired                 map[int]map[OperatorID]bool // Rule 5 (cryptoFake or unknownV) per (layer, op)
	rule6aFired map[OperatorID]bool // Rule 6a Phase-2 equivocation per op (slot-wide)

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
}

// retainedBundle wraps a Phase-1 bundle with the metadata Phase E needs
// to track retention semantics. In 2abOBFT there is no auth-only vs
// regular distinction — the AuthOnly field is dropped relative to the
// previous design.
type retainedBundle struct {
	// Bundle is a deep copy of the retained bundle (defensive against
	// caller-owned slice mutation post-Observe).
	Bundle *Phase1Bundle

	// RetentionEstablishedAt is the offset (from slot_start) at which
	// this retention entry took its current status:
	//
	//   - First observation of a previously-unseen V → set to that
	//     observation's offset.
	//   - Identical re-broadcast (silent dedup) → NOT updated.
	//
	// Useful for tests + diagnostics.
	RetentionEstablishedAt time.Duration
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
	if !operatorInCluster(ownOperatorID, cfg) {
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
		cfg:                    cfg,
		ownOperatorID:          ownOperatorID,
		signer:                 signer,
		tagSigner:              tagSigner,
		ibe:                    ibe,
		clusterPubKey:          clusterPubKey,
		pubKeyShares:           pubKeyShares,
		ibePubKeyShares:        ibePubKeyShares,
		evidenceObserver:       evidenceObserver,
		retainedBundles:        make(map[int]map[OperatorID][]*retainedBundle, K),
		hostVerdict:            make(map[int]map[string]bool, K),
		peerValueMsg:           make(map[OperatorID]*ValueMsg, len(cfg.Operators)),
		peerNoValueMsg:         make(map[OperatorID]*NoValueMsg, len(cfg.Operators)),
		peerCommit:             make(map[OperatorID]*Commit, len(cfg.Operators)),
		valuePool:              make(map[int]map[[32]byte]map[OperatorID]bool, K),
		noValuePool:            make(map[int]map[OperatorID]bool, K),
		sigmaPool:              make(map[int]map[[32]byte]map[OperatorID]Signature, K),
		nrTagPool:              make(map[int]map[OperatorID]Signature, K),
		sigmaLocked:            make([]bool, K),
		sigmaLockedV:           make([]Value, K),
		nrLocked:               make([]bool, K),
		ownPartials:            make(map[int]Signature, K),
		rule1Fired:             make(map[int]map[OperatorID]bool, K),
		rule3Fired:             make(map[int]map[OperatorID]bool, K),
		rule4Fired:             make(map[int]map[OperatorID]bool, K),
		rule5Fired:             make(map[int]map[OperatorID]bool, K),
		rule6aFired:      make(map[OperatorID]bool, len(cfg.Operators)),
		evidenceObserved: make(map[evidenceObservedKey]bool),
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

// Finalize closes the Instance. Idempotent.
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
// signature on nr_tag_j. Used by Phase 3 (reconstruction).
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

// ---------- Per-rule dedup helpers ----------

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

// recordRule3 marks Rule 3 (cross-commit equivocation) as fired for
// (op, layer). Returns true if this is the first observation.
func (i *Instance) recordRule3(op OperatorID, layer int) bool {
	if i.rule3Fired[layer] == nil {
		i.rule3Fired[layer] = make(map[OperatorID]bool)
	}
	if i.rule3Fired[layer][op] {
		return false
	}
	i.rule3Fired[layer][op] = true
	return true
}

// recordRule4 marks Rule 4 (fake encrypted-presence at k > 0) as fired
// for (op, layer). Returns true if this is the first observation.
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

// recordRule5Fired marks Rule 5 (fake plaintext σ at L_0) as fired for
// (op, layer). Returns true if this is the first observation.
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

// removeFromValuePool removes `op` from valuePool[layer][vRoot]. Used by
// the upgrade-detection path to undo a previous noValuePool entry when
// observing a KindValue from the same op (per §Pool aggregation rules /
// Receiver-side robustness — the upgrade KindValue replaces the prior
// KindNoValue's noValuePool contribution).
//
// Note: this function operates on valuePool, but the upgrade path
// actually removes from noValuePool — exposed for symmetry / debugging.
func (i *Instance) removeFromValuePool(layer int, vRoot [32]byte, op OperatorID) {
	if i.valuePool[layer] == nil || i.valuePool[layer][vRoot] == nil {
		return
	}
	delete(i.valuePool[layer][vRoot], op)
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

// valuePoolMaxV returns the (V_root, count) for the V at this layer with
// the most operators in its valuePool. Used by the σ-eligibility trigger
// to determine which V (if any) the cluster has converged on.
//
// At n = 3f+1 (the SSV BFT-tight bound), at most one V can have
// `|valuePool[V]| ≥ qV` per Pigeonhole 2 — so the "max V" is unambiguous
// at quorum. Below quorum, the max V is informational only.
func (i *Instance) valuePoolMaxV(layer int) (vRoot [32]byte, count int) {
	if i.valuePool[layer] == nil {
		return [32]byte{}, 0
	}
	for v, ops := range i.valuePool[layer] {
		if len(ops) > count {
			vRoot = v
			count = len(ops)
		}
	}
	return vRoot, count
}
