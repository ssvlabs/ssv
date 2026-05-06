package obft

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"
)

// CommitState is one operator's per-layer commitment state in the four-state
// model from spec §Phase 1 / Operator commitments. The σ / NR / NV / Defer
// states are local-per-layer; the discriminator that lives on-the-wire is
// "σ-side" (Onion entry at this layer) vs "NR-side" (NR partial in KindNR).
//
// NV (host-not-valid) is operationally identical to NR (silent-leader) —
// both materialize as an IBE partial on the layer's nr_tag. Local state
// distinguishes them only for telemetry / diagnostics.
type CommitState int

const (
	// CommitUndecided — initial state; the operator has not yet observed
	// enough to decide σ or NR at this layer.
	CommitUndecided CommitState = iota

	// CommitSigma — σ-emitted (locked) at this layer. EKM enforces single-
	// σ-V per (slot, layer) and σ-XOR-NR per layer; once locked, the
	// operator may not emit NR/NV nor σ on a different V.
	CommitSigma

	// CommitNRSilent — NR (silent-leader). No peer σ-emit was observed
	// cluster-wide by end-of-Phase-2 NR-decision time, so the leader is
	// presumed silent.
	CommitNRSilent

	// CommitNV — NR (non-validity). Host application returned `not-valid`
	// for this layer's V; operationally identical to NR-silent on the wire.
	CommitNV

	// CommitDeferPartition — V not yet received locally, but peer σ-emit
	// observed cluster-wide. Recoverable within the slot if late re-flood
	// delivers V before end of Phase 2.
	CommitDeferPartition

	// CommitDeferEquivocation — ≥ 2 distinct Phase-1 bundles retained at
	// this layer. Unrecoverable within the slot — re-flood only delivers
	// more bundles, not fewer. Force-NRs at end of Phase 2.
	CommitDeferEquivocation
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
	case CommitDeferPartition:
		return "Defer-partition"
	case CommitDeferEquivocation:
		return "Defer-equivocation"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// Instance is the per-slot OBFT state machine. It accumulates observations
// across Phase 1 (Phase-1 bundles), Phase 2 (peer Onions + NRs), and Phase 3
// (Resolve walk → final certificate gossip).
//
// Lifecycle (driven by the SSV adapter / Scheduler):
//
//  1. NewInstance(cfg, ...)
//  2. Phase 1 — for each layer:
//       a. If local op is the leader: BuildPhase1Bundle(layer, V) → bundle
//          for broadcast.
//       b. ObservePhase1Bundle(b, observedOffset) for each bundle received
//          from peers (or from the local op's own broadcast).
//       c. ApplyHostValidity(layer, V, valid) once the host returns its
//          per-V validity verdict.
//  3. Phase 2 — during [TCommit, TCommit + Delta2]:
//       a. BuildOwnOnion(now) — emit σ partials for σ-eligible layers.
//          May be called multiple times as σ-eligibility transitions late
//          (Defer-partition resolves on late re-flood).
//       b. ObserveOnion(o, observedOffset) for peers' Onions.
//       c. ObserveNR(nr, observedOffset) for peers' NRs (typically arrive
//          near end of Phase 2 from peers that committed NR-side).
//  4. Phase 2 end — at TCommit + Delta2:
//       a. PhaseTwoEnd(now) — apply force-commit rule to all Defer layers.
//       b. BuildOwnNR(now) — emit NR partials for layers committed NR-side.
//  5. Phase 3 — at [TCommit + Delta2, TRoundEnd]:
//       a. Resolve(now) → Output (success) or ErrNoQuorum.
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

	// peerOnions[layer][operator_id] = list of distinct Onion entries seen
	// from this operator at this layer. The first auth-valid entry is
	// canonical for σ-pool / Defer-rule purposes; a second distinct entry
	// is cross-onion equivocation evidence (Rule 3).
	peerOnions map[int]map[OperatorID][]EncryptedLayer

	// peerNR[layer][operator_id] = the operator's NR partial for this layer.
	peerNR map[int]map[OperatorID]Signature

	// Local per-layer state.
	localState   []CommitState
	sigmaLocked  []bool
	sigmaLockedV []Value // when sigmaLocked[k], sigmaLockedV[k] is the V signed
	nrLocked     []bool

	// Own σ partials cached per layer for repeat emission via BuildOwnOnion
	// (multi-emit semantics — KindOnion may be emitted multiple times as
	// σ-eligibility transitions late).
	ownPartials map[int]Signature

	// True after PhaseTwoEnd. Past this point, σ-emit on a previously
	// Undecided / Defer-partition layer is still permitted (the late σ-emit
	// at end-of-Phase-2 contributes to Phase 3 σ-pool reconstruction even
	// if it doesn't propagate to peers in time for their NR-decision), but
	// the operator's local state is force-committed by this call.
	phaseTwoEnded bool

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
// the operator already NR-committed at this layer; ErrEquivocationLocked
// if Defer-due-to-equivocation.
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
	if i.localState[layer] == CommitDeferEquivocation {
		return ErrEquivocationLocked
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
// acceptance window for Phase-1 bundles ([slot_start, T_accept_max]).
func (i *Instance) observedTimeOK(observedOffset time.Duration) bool {
	return observedOffset >= 0 && observedOffset <= i.cfg.AcceptMaxOffset()
}
