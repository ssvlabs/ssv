package obft

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"
)

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
//       a. If local op is the leader: BuildPhase1Bundle(layer, V) → bundle
//          for broadcast.
//       b. ObservePhase1Bundle(b, observedOffset) for each bundle received
//          from peers (or from the local op's own broadcast). Bundles
//          first-observed past T_commit at this operator are not retained.
//       c. ApplyHostValidity(layer, V, valid) once the host returns its
//          per-V validity verdict.
//  3. Phase 2 — at TCommit (single emission):
//       a. BuildOwnCommit() — emit a single KindCommit message carrying σ
//          partials for σ-state layers and NR partials for NR-state layers,
//          based on what was observed by T_commit.
//       b. ObserveCommit(c) for peers' KindCommit messages.
//  4. Phase 3 — from TCommit + Delta2 onward (no hard upper bound here;
//     the runner enforces the slot's relay-submission deadline via ctx):
//       a. Resolve(now) → Output (success) or ErrNoQuorum. Resolve is
//          opportunistic — re-running on late KindCommit arrivals can
//          push σ-pool past qV at a layer that didn't reach on the
//          initial walk, or push NR-pool past qEnc to unlock the next
//          layer's chained decryption (Pigeonhole semantics still hold).
//          RoundEndOffset (= TCommit + Delta2 + Delta3) is a soft per-
//          operator target, not a hard cluster-wide deadline.
//       b. On success: BuildCertificate(out) → broadcast.
//       c. ObserveCertificate(c) for peers' certificates as a fallback
//          submission path when local Resolve fails.
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
func NewInstance(
	cfg *Config,
	ownOperatorID OperatorID,
	signer Signer,
	tagSigner Signer,
	ibe ThresholdIBE,
	clusterPubKey []byte,
	pubKeyShares map[OperatorID][]byte,
	ibePubKeyShares map[OperatorID][]byte,
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
	if tagSigner == nil {
		tagSigner = signer
	}

	K := cfg.K()
	return &Instance{
		cfg:             cfg,
		ownOperatorID:   ownOperatorID,
		signer:          signer,
		tagSigner:       tagSigner,
		ibe:             ibe,
		clusterPubKey:   clusterPubKey,
		pubKeyShares:    pubKeyShares,
		ibePubKeyShares: ibePubKeyShares,
		bundles:         make(map[int]map[OperatorID][]*Phase1Bundle, K),
		hostVerdict:     make(map[int]map[string]bool, K),
		peerOnions:      make(map[int]map[OperatorID][]EncryptedLayer, K),
		peerNR:          make(map[int]map[OperatorID]Signature, K),
		peerCommitHashes: make(map[OperatorID]map[[32]byte]struct{}),
		localState:      make([]CommitState, K),
		sigmaLocked:     make([]bool, K),
		sigmaLockedV:    make([]Value, K),
		nrLocked:        make([]bool, K),
		ownPartials:     make(map[int]Signature),
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

// recordEvidence appends a non-nil evidence entry to the accumulator.
func (i *Instance) recordEvidence(e Evidence) {
	i.evidence = append(i.evidence, e)
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
