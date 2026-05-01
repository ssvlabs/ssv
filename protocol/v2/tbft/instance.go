package tbft

import (
	"bytes"
	"errors"
	"fmt"
)

// Instance is the per-consensus-instance state machine. It accumulates
// observations during Phases 1 and 2, then computes the decision at Phase 3
// via Resolve().
//
// Lifecycle:
//
//  1. NewInstance(cfg, signer, ibe, clusterPubKey)
//  2. Phase 1: caller (or peers via the network adapter) calls
//     ObserveCandidate(layer, value) for each candidate value the operator
//     learns of (either by being the layer's leader and producing it, or
//     by receiving it via gossip).
//  3. Phase 2: caller calls ObserveOnion(o) and ObserveNonReceipt(nr) as
//     onions / non-receipt attestations arrive from peers.
//  4. Phase 3: caller calls Resolve() at the deadline. Resolve walks layers
//     in priority order; first layer where 2f+1 partial sigs on the same
//     value can be assembled wins. Returns the decided Output or an error
//     ("missed slot").
//
// Instance is NOT thread-safe; callers must serialize access. The expected
// SSV adapter runs each Instance in its own goroutine fed by a message
// queue.
type Instance struct {
	cfg *Config

	// signer signs candidate values (Phase-2 onion contents) and verifies
	// per-partial sigs during Resolve. For SSV, this is the BLSSigner
	// (herumi-backed, Eth2 DST).
	signer Signer

	// tagSigner signs no-quorum tags (NonReceiptAttestations) and
	// aggregates them to derive IBE decryption keys. For real IBE under
	// the "DST trick" (see docs/IBE-INTEGRATION.md), this is the
	// KyberSigner (kyber-backed, drand DST). If nil, falls back to
	// `signer` — sufficient when paired with SignerGatedIBE / StubIBE
	// where one DST suffices.
	tagSigner Signer

	ibe           ThresholdIBE
	clusterPubKey []byte

	// pubKeyShares maps each operator's ID to the BLS pubkey share used
	// to verify partial signatures from that operator. For Option A
	// (reuse validator threshold key) these are the operator-share
	// pubkeys derived from the validator's split key. The Instance uses
	// them in tryReconstructLayer to reject forged or malformed partial
	// signature contributions.
	pubKeyShares map[OperatorID][]byte

	// ibePubKeyShares maps each operator's ID to their IBE pubkey share
	// (the cluster IBE polynomial evaluated at x=opID). When non-nil,
	// ObserveNonReceipt verifies each incoming NonReceipt partial sig
	// against the matching pubkey share before storing it — catching
	// byzantine NR partials at observe time rather than letting them
	// silently corrupt the aggregated IBE decryption key.
	//
	// Optional: nil under Option A (no separate IBE keypair). Set via
	// SetIBEPubKeyShares after construction; left nil for legacy /
	// stub-signer tests where the verification adds no value.
	ibePubKeyShares map[OperatorID][]byte

	// Phase 1: candidate values per layer (typically only the leader's
	// view at each layer). For a given layer there is at most one value
	// the operator considers "the canonical candidate." Equivocation by
	// a leader is detected during Resolve when peers' onions disagree
	// on the layer's signed value.
	candidates map[int]Value

	// Phase 2: received onions, keyed by their producing operator ID.
	onions map[OperatorID]*Onion

	// Phase 2: received non-receipt attestations, keyed by (layer, op).
	nonReceipts map[int]map[OperatorID]Signature

	// inconsistencyFaults logs (operator, layer) pairs where an operator
	// both put a non-empty signature in their onion's layer AND broadcast
	// a non-receipt attestation for the same layer. This is provably
	// inconsistent and slashable in principle. The protocol just records
	// the fault; punishment is an out-of-band concern.
	inconsistencyFaults []InconsistencyFault
}

// InconsistencyFault is a recorded protocol-level fault. Operator i emitted
// both a positive signature at layer L AND a non-receipt attestation for
// the same layer — provably contradictory.
type InconsistencyFault struct {
	OperatorID OperatorID
	Layer      int
}

// NewInstance creates a new consensus instance with `signer` used for both
// value-signing AND tag-signing. Equivalent to NewInstanceWithTagSigner
// with `tagSigner == nil`. Suitable when the IBE primitive accepts the
// same Signer's aggregates as decryption keys (e.g. SignerGatedIBE).
//
//   - `clusterPubKey` is the validator's BLS pubkey (the IBE trust anchor).
//   - `pubKeyShares` maps operator IDs to their pubkey shares; used by
//     Resolve to verify per-operator partial signatures via
//     `Signer.VerifyPartial`.
func NewInstance(
	cfg *Config,
	signer Signer,
	ibe ThresholdIBE,
	clusterPubKey []byte,
	pubKeyShares map[OperatorID][]byte,
) (*Instance, error) {
	return NewInstanceWithTagSigner(cfg, signer, nil, ibe, clusterPubKey, pubKeyShares)
}

// NewInstanceWithTagSigner creates a new consensus instance with separate
// signers for value-signing and tag-signing. This is the constructor used
// when the IBE primitive expects a *different* signature format for its
// decryption keys than the validator-output sigs (e.g. real cryptographic
// IBE via TLockIBE, where IBE-tag sigs are kyber-format and validator-output
// sigs are herumi-format). See docs/IBE-INTEGRATION.md.
//
// If `tagSigner` is nil, falls back to using `signer` for both purposes —
// matching the behavior of NewInstance.
func NewInstanceWithTagSigner(
	cfg *Config,
	signer Signer,
	tagSigner Signer,
	ibe ThresholdIBE,
	clusterPubKey []byte,
	pubKeyShares map[OperatorID][]byte,
) (*Instance, error) {
	if cfg == nil || signer == nil || ibe == nil {
		return nil, errors.New("tbft: nil config / signer / ibe")
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("tbft: invalid config: %w", err)
	}
	if pubKeyShares == nil {
		return nil, errors.New("tbft: nil pubKeyShares (need at least an empty map)")
	}
	if tagSigner == nil {
		tagSigner = signer
	}
	return &Instance{
		cfg:           cfg,
		signer:        signer,
		tagSigner:     tagSigner,
		ibe:           ibe,
		clusterPubKey: clusterPubKey,
		pubKeyShares:  pubKeyShares,
		candidates:    make(map[int]Value),
		onions:        make(map[OperatorID]*Onion),
		nonReceipts:   make(map[int]map[OperatorID]Signature),
	}, nil
}

// Config returns the instance's config (read-only).
func (i *Instance) Config() *Config { return i.cfg }

// SetIBEPubKeyShares enables per-NonReceipt-attestation partial-sig
// verification at ObserveNonReceipt time. Each entry maps an operator's
// ID to their IBE pubkey share (cluster IBE polynomial evaluated at
// x=opID). When set, byzantine NR partials are rejected as they arrive
// instead of silently corrupting the aggregated decryption key.
//
// Pass nil (or never call) to skip verification — appropriate for
// Option A (no separate IBE keypair) and stub-signer tests.
func (i *Instance) SetIBEPubKeyShares(shares map[OperatorID][]byte) {
	i.ibePubKeyShares = shares
}

// ObserveCandidate records a candidate value at a given layer. Caller must
// have already validated `value` against the application-specific rules
// (e.g. SSV's slashing-protection, value-checker logic). The first
// observation for a layer wins; later observations are silently ignored
// (an honest leader broadcasts at most one value per layer).
func (i *Instance) ObserveCandidate(layer int, value Value) error {
	if layer < 0 || layer >= i.cfg.K() {
		return fmt.Errorf("tbft: candidate layer %d out of range [0, %d)", layer, i.cfg.K())
	}
	if _, exists := i.candidates[layer]; exists {
		return nil
	}
	i.candidates[layer] = append(Value{}, value...)
	return nil
}

// ObserveOnion records a peer's onion. Re-observations from the same
// operator are silently ignored (operators broadcast at most one onion
// per instance). Returns an error for malformed onions; call sites can
// use ValidateOnion directly for the structural check without recording.
func (i *Instance) ObserveOnion(o *Onion) error {
	if err := ValidateOnion(o, i.cfg); err != nil {
		return err
	}
	if _, exists := i.onions[o.OperatorID]; exists {
		return nil
	}
	i.onions[o.OperatorID] = o
	return nil
}

// ObserveNonReceipt records a peer's non-receipt attestation. Re-observations
// from the same operator at the same layer are silently ignored.
//
// When ibePubKeyShares is set (typically post-DKG, Option B), this
// method verifies the NR partial sig against the operator's IBE pubkey
// share before storing — catching byzantine garbage NRs that would
// otherwise silently corrupt the aggregated decryption key.
func (i *Instance) ObserveNonReceipt(nr *NonReceiptAttestation) error {
	if err := ValidateNonReceipt(nr, i.cfg); err != nil {
		return err
	}
	if i.ibePubKeyShares != nil {
		pubShare, ok := i.ibePubKeyShares[nr.OperatorID]
		if !ok {
			return fmt.Errorf("tbft: no IBE pubkey share for operator %d", nr.OperatorID)
		}
		tag := NoQuorumTag(i.cfg.ClusterID, i.cfg.Height, nr.Layer)
		if !i.tagSigner.VerifyPartial(pubShare, tag, nr.PartialSig) {
			return fmt.Errorf("tbft: non-receipt partial sig from op %d failed verification", nr.OperatorID)
		}
	}
	if i.nonReceipts[nr.Layer] == nil {
		i.nonReceipts[nr.Layer] = make(map[OperatorID]Signature)
	}
	if _, exists := i.nonReceipts[nr.Layer][nr.OperatorID]; exists {
		return nil
	}
	i.nonReceipts[nr.Layer][nr.OperatorID] = append(Signature{}, nr.PartialSig...)
	return nil
}

// InconsistencyFaults returns the list of operators caught signing both a
// positive contribution and a non-receipt attestation for the same layer.
// Detected during Resolve.
func (i *Instance) InconsistencyFaults() []InconsistencyFault {
	out := make([]InconsistencyFault, len(i.inconsistencyFaults))
	copy(out, i.inconsistencyFaults)
	return out
}

// BuildOwnOnion is a convenience wrapper over BuildOnion that uses the
// candidates already observed via ObserveCandidate. The SSV runner
// adapter will typically use this rather than the standalone BuildOnion
// function.
//
// Signing uses i.signer, which is bound to this operator's share at
// Instance construction.
func (i *Instance) BuildOwnOnion(operatorID OperatorID) (*Onion, error) {
	return BuildOnion(i.cfg, operatorID, i.clusterPubKey, i.candidates, i.signer, i.ibe)
}

// BuildOwnNonReceipts returns a non-receipt attestation for each layer in
// [0, K-1) where this operator does NOT have a candidate. These are the
// non-receipts the operator should broadcast given its current view.
//
// Tag-signing uses i.tagSigner (which falls back to i.signer if not
// separately configured). Under the DST-trick approach, tagSigner is the
// kyber-backed Signer so the resulting partial sigs can aggregate into
// a valid IBE decryption key.
//
// Per the equivocation-handling rule: if the operator has detected leader
// equivocation at a layer (multiple distinct values broadcast), the caller
// should NOT have called ObserveCandidate for that layer — leaving it
// absent from i.candidates means BuildOwnNonReceipts will produce a
// non-receipt for it, which is the correct behavior.
func (i *Instance) BuildOwnNonReceipts(operatorID OperatorID) ([]*NonReceiptAttestation, error) {
	K := i.cfg.K()
	var out []*NonReceiptAttestation
	for layer := 0; layer < K-1; layer++ {
		if _, has := i.candidates[layer]; has {
			continue
		}
		nr, err := BuildNonReceipt(i.cfg, operatorID, layer, i.tagSigner)
		if err != nil {
			return nil, err
		}
		out = append(out, nr)
	}
	return out, nil
}

// Resolve runs the Phase 3 decryption walk: for each layer in priority
// order, attempt to reconstruct a quorum positive signature on the layer's
// candidate value; if that fails, attempt to derive the next layer's
// decryption key from non-receipt attestations.
//
// Returns:
//   - (*Output, nil)        — a layer reached positive quorum; output is the
//     decided value + reconstructed full BLS signature.
//   - (nil, ErrNoQuorum)    — exhausted all K layers without reaching positive
//     quorum or unlocking the next layer; the slot is missed.
//   - (nil, other error)    — internal error (malformed input, crypto failure).
//
// Inconsistency faults (operators emitting both positive contributions and
// non-receipts for the same layer) are detected and recorded as a side
// effect; they don't affect the output decision but can be retrieved via
// InconsistencyFaults().
func (i *Instance) Resolve() (*Output, error) {
	K := i.cfg.K()

	// Decryption keys for layers > 0. Layer 0 is plaintext (no key needed);
	// layer k for k > 0 is decrypted using layerKeys[k].
	layerKeys := make(map[int][]byte)

	for layer := 0; layer < K; layer++ {
		// Check inconsistency at this layer (operators in both the positive
		// onion-contributors set AND the non-receipt set).
		i.detectInconsistencyAt(layer)

		// Try to assemble positive quorum for this layer.
		out, err := i.tryReconstructLayer(layer, layerKeys[layer])
		if err != nil {
			return nil, fmt.Errorf("tbft: layer %d reconstruction: %w", layer, err)
		}
		if out != nil {
			return out, nil
		}

		// Positive quorum unreachable at this layer. If there's a next
		// layer, try to derive its decryption key from non-receipt
		// attestations on this layer's no-quorum tag.
		if layer == K-1 {
			break // last layer — no further fallback
		}

		nextKey, err := i.tryDeriveNextLayerKey(layer)
		if err != nil {
			return nil, fmt.Errorf("tbft: layer %d non-receipt aggregation: %w", layer, err)
		}
		if nextKey == nil {
			// Neither positive quorum at this layer nor a quorum of
			// non-receipts to advance. Stuck.
			return nil, ErrNoQuorum
		}
		layerKeys[layer+1] = nextKey
	}

	return nil, ErrNoQuorum
}

// ErrNoQuorum is returned by Resolve when neither a positive quorum at any
// layer nor a non-receipt quorum sufficient to unlock the next layer was
// reached. The slot is missed.
var ErrNoQuorum = errors.New("tbft: no quorum reached at any layer")

// sigGroup holds partial signatures grouped by the value they sign at a
// given layer. Multiple groups at the same layer indicate leader
// equivocation: the leader broadcast different values to different
// operators, and each subset signed what they received.
//
// The protocol counts each group independently. A group reaching quorum
// (2f+1) wins the layer. If no group does, the layer is "stuck" — and
// can only be advanced past if 2f+1 non-receipt attestations exist for
// the layer's tag. In production, honest operators are expected to
// detect equivocation during Phase 1 (by observing multiple distinct
// candidate broadcasts from the same leader) and treat the layer as
// non-receipt — i.e. emit a NonReceiptAttestation rather than a
// positive sig. This converts equivocation into a clean non-receipt
// quorum that unlocks the next layer.
type sigGroup struct {
	value    Value
	partials map[OperatorID]Signature
}

// tryReconstructLayer attempts positive-quorum reconstruction at `layer`.
// Returns:
//   - (*Output, nil)    — quorum reached, output produced
//   - (nil, nil)        — no quorum (caller should attempt fallback)
//   - (nil, error)      — internal error (e.g. decryption malformed)
//
// `decryptionKey` is nil for layer 0 (plaintext) and the aggregated layer-k-1
// no-quorum-tag signature for layer k > 0.
func (i *Instance) tryReconstructLayer(layer int, decryptionKey []byte) (*Output, error) {
	// Collect partial sigs at this layer from all received onions, grouped
	// by the value each operator signed (visible in EncryptedLayer.Value).
	// Different groups at the same layer indicate leader equivocation;
	// each group is counted independently.
	groups := make([]*sigGroup, 0, 1)

	for opID, o := range i.onions {
		if layer >= len(o.Layers) {
			continue
		}
		el := o.Layers[layer]
		if len(el.Value) == 0 || len(el.Ciphertext) == 0 {
			continue // operator did not contribute at this layer
		}

		var partial Signature
		if layer == 0 {
			partial = Signature(el.Ciphertext)
		} else {
			pt, err := i.ibe.Decrypt(el.Ciphertext, decryptionKey)
			if err != nil {
				// Undecryptable with our derived key — skip this
				// contribution rather than failing the whole resolution.
				continue
			}
			partial = Signature(pt)
		}

		// Verify the partial signs the value the operator claims it does.
		// Reject contributions that don't (forged or malformed onions).
		// pubKeyShare for an unknown operator is nil; VerifyPartial
		// returns false on nil/missing shares (defensive — peers might
		// gossip onions from operators not in our cluster view).
		pubShare := i.pubKeyShares[opID]
		if !i.signer.VerifyPartial(pubShare, el.Value, partial) {
			continue
		}
		addToGroup(&groups, el.Value, opID, partial)
	}

	// Pick the group with the most partials (typically there's at most one
	// group per layer; multiple groups indicate equivocation).
	var winning *sigGroup
	for _, g := range groups {
		if winning == nil || len(g.partials) > len(winning.partials) {
			winning = g
		}
	}
	if winning == nil || len(winning.partials) < i.cfg.QV() {
		return nil, nil
	}

	// Aggregate the winning group's partials into the full signature.
	full, err := i.signer.AggregatePartials(winning.partials)
	if err != nil {
		return nil, fmt.Errorf("aggregate partials: %w", err)
	}

	return &Output{
		Layer:     layer,
		Value:     append(Value{}, winning.value...),
		Signature: full,
	}, nil
}

// tryDeriveNextLayerKey attempts to aggregate qEnc non-receipt
// attestations on NoQuorumTag(layer). Returns the derived decryption
// key for layer+1, or nil if the NR-quorum threshold wasn't reached.
//
// qEnc = qV = 2f+1 per docs/TBFT.md "Why it's safe" — the unified
// threshold gives cryptographic safety against byzantine cross-signing.
// Aggregating 2f+1 IBE-share partials yields the IBE master signature
// on the tag, directly usable as a decryption key for layer+1. Under
// Option A (DST-trick on validator key), the same threshold is enforced
// by the validator-share's natural 2f+1 quorum; under Option B (separate
// IBE keypair) the DKG runs at 2f+1 to match (see
// docs/TBFT-DKG-TASKS.md).
func (i *Instance) tryDeriveNextLayerKey(layer int) ([]byte, error) {
	partials := i.nonReceipts[layer]
	if len(partials) < i.cfg.QEnc() {
		return nil, nil
	}
	// Aggregate the non-receipt partials into a full BLS signature on the
	// no-quorum tag. Under the DST-trick approach, this aggregate IS the
	// IBE decryption key for layer+1 directly — the IBE primitive's
	// encryption is bound to a message the cluster's tagSigner-aggregated
	// threshold signature signs.
	full, err := i.tagSigner.AggregatePartials(partials)
	if err != nil {
		return nil, err
	}
	return []byte(full), nil
}

// detectInconsistencyAt records (operator, layer) pairs where an operator
// is in both the positive-onion-contributors set and the non-receipt set
// for the same layer.
func (i *Instance) detectInconsistencyAt(layer int) {
	if layer >= i.cfg.K()-1 {
		// No non-receipts exist for the last layer.
		return
	}
	nrSet := i.nonReceipts[layer]
	if len(nrSet) == 0 {
		return
	}
	for opID, o := range i.onions {
		if layer >= len(o.Layers) {
			continue
		}
		if len(o.Layers[layer].Ciphertext) == 0 {
			continue
		}
		if _, hasNR := nrSet[opID]; hasNR {
			i.inconsistencyFaults = append(i.inconsistencyFaults, InconsistencyFault{
				OperatorID: opID,
				Layer:      layer,
			})
		}
	}
}

// addToGroup adds (opID, partial) to the group keyed by `value`, creating
// the group if needed.
func addToGroup(groups *[]*sigGroup, value Value, opID OperatorID, partial Signature) {
	for _, g := range *groups {
		if bytes.Equal(g.value, value) {
			g.partials[opID] = partial
			return
		}
	}
	g := &sigGroup{
		value:    append(Value{}, value...),
		partials: map[OperatorID]Signature{opID: partial},
	}
	*groups = append(*groups, g)
}
